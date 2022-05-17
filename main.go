package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/mail"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	flag "github.com/spf13/pflag"
	"golang.org/x/text/encoding/ianaindex"
)

func (lk *LetterKnife) debugf(format string, args ...interface{}) {
	if lk.ModeDebug {
		log.Printf("debug: "+format, args...)
	}
}

func fatalf(format string, args ...interface{}) {
	_, file, line, _ := runtime.Caller(1)
	log.Printf("%s:%d", file, line)
	log.Printf("fatal: "+format, args...)
	os.Exit(1)
}

var mimeDecoder = new(mime.WordDecoder)

func init() {
	mimeDecoder.CharsetReader = func(charset string, input io.Reader) (io.Reader, error) {
		enc, err := ianaindex.MIME.Encoding(charset)
		if err != nil {
			return nil, err
		}
		return enc.NewDecoder().Reader(input), nil
	}
}

func extensionsByType(typ string) string {
	switch typ {
	case "text/html":
		return ".html"
	case "text/plain":
		return ".txt"
	}

	exts, _ := mime.ExtensionsByType(typ)
	if len(exts) > 0 {
		return exts[0]
	}

	return ".bin"
}

var delmiter = "\n"

type LetterKnife struct {
	Delmiter string

	In  io.Reader
	Out io.Writer

	ShortcutFrom    string
	ShortcutSubject string
	ShortcutHTML    bool
	ShortcutPlain   bool

	MatchAddress string
	MatchHeader  string

	SelectPart       string
	SelectAttachment string

	PrintContent bool
	PrintHeader  string
	PrintRaw     bool
	SaveFile     bool

	ModeDebug bool
}

func (lk *LetterKnife) ParseFlags(args []string) error {
	flags := flag.NewFlagSet("letterknife", flag.ExitOnError)

	flags.StringVar(&lk.ShortcutFrom, "from", "", "Shortcut for --match-address 'From:`<pattern>`'")
	flags.StringVar(&lk.ShortcutSubject, "subject", "", "Shortcut for --match-header 'Subject:`<pattern>`'")
	flags.BoolVar(&lk.ShortcutHTML, "html", false, "Shortcut for --select-part text/html")
	flags.BoolVar(&lk.ShortcutPlain, "plain", false, "Shortcut for --select-part text/plain")

	// TODO: make multiple
	flags.StringVar(&lk.MatchAddress, "match-address", "", "Filter: address header `<header>:<pattern>` eg. \"From:*@example.com\"")
	flags.StringVar(&lk.MatchHeader, "match-header", "", "Filter: header `<header>:<pattern>` eg. \"Subject:foobar\"")

	flags.StringVar(&lk.SelectPart, "select-part", "", "Select: non-attachment parts by `<content-type>`")
	flags.StringVar(&lk.SelectAttachment, "select-attachment", "", "Select: attachments by `<content-type>`")

	flags.BoolVar(&lk.PrintContent, "print-content", true, "Action: print decoded content")
	flags.StringVar(&lk.PrintHeader, "print-header", "", "Action: print `<header>`")
	flags.BoolVar(&lk.PrintRaw, "print-raw", false, "Action: print raw input as-is")
	flags.BoolVar(&lk.SaveFile, "save-file", false, "Action: save parts as files and print their paths")

	flags.BoolVar(&lk.ModeDebug, "debug", false, "enable debug logging")

	flags.SortFlags = false

	return flags.Parse(args)
}

// --from, --subject, --html, --plain
// --match-address From:...
// --match-header Subject:...
// --select-part text/html
// --select-attachment application/pdf
// --print-content
// --print-json // TODO
// --save-file
// --list-parts // ???
// --debug, --quiet // TODO
func main() {
	l := &LetterKnife{}
	l.Delmiter = "\n"
	l.In = os.Stdin
	l.Out = os.Stdout

	err := l.ParseFlags(os.Args[1:])
	if err != nil {
		fatalf("%v", err)
	}

	l.Run()
}

func (lk *LetterKnife) Run() {
	r := lk.In
	w := lk.Out

	// holds whole input
	var in bytes.Buffer

	msg, err := mail.ReadMessage(io.TeeReader(r, &in))
	if err != nil {
		fatalf("failed to read message: %v", err)
	}

	pass := true

	if lk.ShortcutFrom != "" {
		lk.MatchAddress = "From:" + lk.ShortcutFrom
	}
	if lk.ShortcutSubject != "" {
		lk.MatchHeader = "Subject:" + lk.ShortcutSubject
	}
	if lk.ShortcutHTML {
		lk.SelectPart = "text/html"
	}
	if lk.ShortcutPlain {
		lk.SelectPart = "text/plain"
	}

	if lk.MatchAddress != "" {
		ok, err := lk.checkMatch(msg.Header, lk.MatchAddress, true)
		if err != nil {
			fatalf("checkMatch(%s): %v", lk.MatchAddress, err)
		}
		if !ok {
			pass = false
		}
	}

	if lk.MatchHeader != "" {
		ok, err := lk.checkMatch(msg.Header, lk.MatchHeader, false)
		if err != nil {
			fatalf("checkMatch(%s): %v", lk.MatchHeader, err)
		}
		if !ok {
			pass = false
		}
	}

	if !pass {
		fatalf("matching header failed")
	}

	// TODO: newMessagePart()
	ct := msg.Header.Get("Content-Type")
	mt, params, err := mime.ParseMediaType(ct)
	if err != nil {
		fatalf("parsing content-type %q: %v", ct, err)
	}

	wholePart := &messagePart{
		header: msg.Header,
		// special case: includes headers along with body
		body:            &in,
		mediaType:       mt,
		mediaTypeParams: params,
	}

	rootPart, err := buildPartTree(msg.Header, msg.Body)
	if err != nil {
		fatalf("while building tree: %v", err)
	}

	var selectedParts []*messagePart
	if lk.SelectPart != "" {
		pp, err := lk.selectParts(rootPart, lk.SelectPart, false)
		if err != nil {
			fatalf("%v", err)
		}
		selectedParts = append(selectedParts, pp...)
	}

	if lk.SelectAttachment != "" {
		pp, err := lk.selectParts(rootPart, lk.SelectAttachment, true)
		if err != nil {
			fatalf("%v", err)
		}
		selectedParts = append(selectedParts, pp...)
	}

	if lk.SelectPart != "" || lk.SelectAttachment != "" {
		if len(selectedParts) == 0 {
			fatalf("selecting parts failed")
		}
	}

	if lk.PrintHeader != "" || lk.SaveFile || lk.PrintRaw {
		lk.PrintContent = false
	}

	// If no part is selected and --print-content is specified,
	// then it should be treated as --print-raw.
	if len(selectedParts) == 0 && lk.PrintContent {
		lk.PrintContent = false
		lk.PrintRaw = true
	}

	if len(selectedParts) == 0 {
		selectedParts = []*messagePart{wholePart}
	}

	if lk.PrintContent {
		for _, mp := range selectedParts {
			_, err = io.Copy(w, mp)
			if err != nil {
				fatalf("%v", err)
			}
			fmt.Fprint(w, delmiter)
		}
	}

	if lk.PrintRaw {
		_, err = io.Copy(w, &in)
		if err != nil {
			fatalf("%v", err)
		}
	}

	if lk.PrintHeader != "" {
		for _, mp := range selectedParts {
			s, err := mimeDecoder.DecodeHeader(mp.header.Get(lk.PrintHeader))
			if err != nil {
				fatalf("decoding header %q failed: %v", lk.PrintHeader, err)
			}
			fmt.Fprint(w, s)
			fmt.Fprint(w, delmiter)
		}
	}

	if lk.SaveFile {
		dir, err := os.MkdirTemp("", "")
		if err != nil {
			fatalf("while creating temporary directory: %v", err)
		}

		for _, mp := range selectedParts {
			filename, _ := mp.attachmentFilename()

			var f *os.File
			if filename != "" {
				path := filepath.Join(dir, filename)
				f, err = os.Create(path)
				if err != nil {
					fatalf("creating file: %v", err)
				}
			} else {
				ext := extensionsByType(mp.mediaType)
				if mp == wholePart {
					ext = ".eml"
				}

				f, err = os.CreateTemp(dir, "*"+ext)
				if err != nil {
					fatalf("creating file: %v", err)
				}
			}

			_, err = io.Copy(f, mp)
			if err != nil {
				fatalf("%v", err)
			}

			f.Close()
			fmt.Fprint(w, f.Name())
			fmt.Fprint(w, delmiter)
		}
	}
}

type messagePart struct {
	header          mail.Header
	mediaType       string
	mediaTypeParams map[string]string

	r io.Reader

	// either is defined
	body     *bytes.Buffer
	subparts []*messagePart

	disposition       string
	dispositionParams map[string]string
}

type errWrappedReader struct {
	message string
	r       io.Reader
}

// Read implements io.Reader
func (r *errWrappedReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	if err == io.EOF {
		return n, err
	} else if err != nil {
		return n, fmt.Errorf("%s: %w", r.message, err)
	}

	return n, nil
}

// Read implements io.Reader
func (m *messagePart) Read(p []byte) (n int, err error) {
	if m.r == nil {
		var r io.Reader = m.body

		if strings.EqualFold(m.header.Get("Content-Transfer-Encoding"), "base64") {
			r = base64.NewDecoder(base64.StdEncoding, r)
		}

		if charset := m.mediaTypeParams["charset"]; charset != "" {
			enc, err := ianaindex.MIME.Encoding(charset)
			if err != nil {
				return 0, fmt.Errorf("failed to build charset %q decoder: %v", charset, err)
			}

			r = enc.NewDecoder().Reader(r)
			r = &errWrappedReader{
				message: "decoding " + charset,
				r:       r,
			}
		}

		m.r = r
	}

	return m.r.Read(p)
}

func (m *messagePart) isMultipart() bool {
	return m.body == nil
}

func (m *messagePart) attachmentFilename() (string, bool) {
	if m.disposition != "attachment" {
		return "", false
	}
	return m.dispositionParams["filename"], true
}

func buildPartTree(header mail.Header, body io.Reader) (*messagePart, error) {
	ct := header.Get("Content-Type")
	mt, params, err := mime.ParseMediaType(ct)
	if err != nil {
		return nil, fmt.Errorf("parsing content-type %q: %v", ct, err)
	}

	part := messagePart{
		header:          header,
		mediaType:       mt,
		mediaTypeParams: params,
	}

	if strings.HasPrefix(mt, "multipart/") && params["boundary"] != "" {
		mr := multipart.NewReader(body, params["boundary"])
		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			} else if err != nil {
				return nil, fmt.Errorf("reading multipart: %v", err)
			}

			subpart, err := buildPartTree(mail.Header(p.Header), p)
			if err != nil {
				return nil, err
			}
			part.subparts = append(part.subparts, subpart)
		}
		return &part, nil
	}

	part.disposition, part.dispositionParams, _ = mime.ParseMediaType(header.Get("Content-Disposition"))
	part.body = new(bytes.Buffer)
	_, err = io.Copy(part.body, body)
	return &part, err
}

func (lk *LetterKnife) visitParts(mp *messagePart, visit func(*messagePart) error) error {
	lk.debugf("visitParts: %v sub=%v", mp.header.Get("Content-Type"), mp.subparts)

	if mp.isMultipart() {
		for _, p := range mp.subparts {
			if err := lk.visitParts(p, visit); err != nil {
				return err
			}
		}
		return nil
	}

	return visit(mp)
}

func (lk *LetterKnife) selectParts(mp *messagePart, mediaTypeSpec string, isAttachmentSpec bool) ([]*messagePart, error) {
	parts := []*messagePart{}
	err := lk.visitParts(mp, func(mp *messagePart) error {
		_, isAttachment := mp.attachmentFilename()
		if isAttachment != isAttachmentSpec {
			return nil
		}

		ok, err := testPattern(mp.mediaType, mediaTypeSpec)
		if err != nil {
			return err
		}

		if ok {
			parts = append(parts, mp)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return parts, nil
}

func (lk *LetterKnife) checkMatch(h mail.Header, in string, isAddr bool) (bool, error) {
	// TODO: fail if header does not exist
	p := strings.IndexByte(in, ':')
	if p == -1 {
		return false, fmt.Errorf("must be in the form of `header:pattern`: %q", in)
	}
	header, pattern := in[0:p], in[p+1:]

	var values []string
	if isAddr {
		addrs, err := (&mail.AddressParser{WordDecoder: mimeDecoder}).ParseList(h.Get(header))
		if err != nil {
			return false, fmt.Errorf("parsing header %s: as addresses: %v", header, err)
		}
		values = make([]string, len(addrs))
		for i, addr := range addrs {
			values[i] = addr.Address
		}
	} else {
		values = []string{h.Get(header)}
	}

	for _, value := range values {
		value, err := mimeDecoder.DecodeHeader(value)
		if err != nil {
			return false, err
		}

		lk.debugf("test %s: %q against %q", header, value, pattern)

		ok, err := testPattern(value, pattern)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}

	return false, nil
}

var rxPattern = regexp.MustCompile(`(\*|.+?)`)

func regexpFromPattern(pattern string) (*regexp.Regexp, error) {
	if pattern[0] == '/' && pattern[len(pattern)-1] == '/' {
		return regexp.Compile(pattern[1 : len(pattern)-1])
	}

	p := rxPattern.ReplaceAllStringFunc(pattern, func(s string) string {
		if s == "*" {
			return ".+?"
		} else {
			return regexp.QuoteMeta(s)
		}
	})
	return regexp.Compile("^" + p + "$")
}

func testPattern(value, pattern string) (bool, error) {
	if strings.IndexByte(pattern, '*') == -1 && (pattern[0] != '/' && pattern[len(pattern)-1] != '/') {
		return value == pattern, nil
	}

	rx, err := regexpFromPattern(pattern)
	if err != nil {
		return false, err
	}

	return rx.MatchString(value), nil
}

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
	"strings"

	flag "github.com/spf13/pflag"
	"golang.org/x/text/encoding/ianaindex"
)

var modeDebug bool

func debugf(format string, args ...interface{}) {
	if modeDebug {
		log.Printf("debug: "+format, args...)
	}
}

func fatalf(format string, args ...interface{}) {
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
	delmiter := "\n"
	var (
		shortcutFrom    = flag.String("from", "", "Shortcut for --match-address 'From:`<pattern>`'")
		shortcutSubject = flag.String("subject", "", "Shortcut for --match-header 'Subject:`<pattern>`'")
		shortcutHTML    = flag.Bool("html", false, "Shortcut for --select-part text/html")
		shortcutPlain   = flag.Bool("plain", false, "Shortcut for --select-part text/plain")

		// TODO: make multiple
		matchAddress = flag.String("match-address", "", "Filter: address header `<header>:<pattern>` eg. \"From:*@example.com\"")
		matchHeader  = flag.String("match-header", "", "Filter: header `<header>:<pattern>` eg. \"Subject:foobar\"")

		selectPart       = flag.String("select-part", "", "Select: non-attachment parts by `<content-type>`")
		selectAttachment = flag.String("select-attachment", "", "Select: attachments by `<content-type>`")

		printContent = flag.Bool("print-content", true, "Action: print decoded content")
		printHeader  = flag.String("print-header", "", "Action: print `<header>`")
		saveFile     = flag.Bool("save-file", false, "Action: save parts as files and print their paths")
	)

	flag.BoolVar(&modeDebug, "debug", false, "enable debug logging")

	flag.CommandLine.SortFlags = false
	flag.Parse()

	// holds whole input
	var in bytes.Buffer

	msg, err := mail.ReadMessage(io.TeeReader(os.Stdin, &in))
	if err != nil {
		fatalf("failed to read message: %v", err)
	}

	pass := true

	if *shortcutFrom != "" {
		*matchAddress = "From:" + *shortcutFrom
	}
	if *shortcutSubject != "" {
		*matchHeader = "Subject:" + *shortcutSubject
	}
	if *shortcutHTML {
		*selectPart = "text/html"
	}
	if *shortcutPlain {
		*selectPart = "text/plain"
	}

	if *matchAddress != "" {
		ok, err := checkMatch(msg.Header, *matchAddress, true)
		if err != nil {
			fatalf("checkMatch(%s): %v", *matchAddress, err)
		}
		if !ok {
			pass = false
		}
	}

	if *matchHeader != "" {
		ok, err := checkMatch(msg.Header, *matchHeader, false)
		if err != nil {
			fatalf("checkMatch(%s): %v", *matchHeader, err)
		}
		if !ok {
			pass = false
		}
	}

	if !pass {
		fatalf("match failed")
	}

	wholePart := &mailPart{
		header: msg.Header,
		// special case: includes headers along with body
		body:   &in,
		isRoot: true,
	}

	rootPart, err := buildPartTree(msg.Header, msg.Body)
	if err != nil {
		fatalf("while building tree: %v", err)
	}

	var selectedParts []*mailPart
	if *selectPart != "" {
		pp, err := selectParts(rootPart, *selectPart, false)
		if err != nil {
			log.Fatal(err)
		}
		selectedParts = append(selectedParts, pp...)
	}

	if *selectAttachment != "" {
		pp, err := selectParts(rootPart, *selectAttachment, true)
		if err != nil {
			log.Fatal(err)
		}
		selectedParts = append(selectedParts, pp...)
	}

	if *selectPart != "" || *selectAttachment != "" {
		if len(selectedParts) == 0 {
			fatalf("select failed")
		}
	} else {
		selectedParts = []*mailPart{wholePart}
	}

	if *printHeader != "" || *saveFile {
		*printContent = false
	}

	if *printContent {
		for _, mp := range selectedParts {
			io.Copy(os.Stdout, mp.reader())
			fmt.Print(delmiter)
		}
	}

	if *printHeader != "" {
		for _, mp := range selectedParts {
			s, err := mimeDecoder.DecodeHeader(mp.header.Get(*printHeader))
			if err != nil {
				fatalf("decoding header %q failed: %v", *printHeader, err)
			}
			fmt.Print(s)
			fmt.Print(delmiter)
		}
	}

	if *saveFile {
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
				if mp.isRoot {
					ext = ".eml"
				}

				f, err = os.CreateTemp(dir, "*"+ext)
				if err != nil {
					fatalf("creating file: %v", err)
				}
			}

			io.Copy(f, mp.reader())
			f.Close()
			fmt.Print(f.Name())
			fmt.Print(delmiter)
		}
	}
}

type mailPart struct {
	header    mail.Header
	mediaType string

	isRoot bool

	// either is defined
	body     *bytes.Buffer
	subparts []*mailPart

	disposition       string
	dispositionParams map[string]string
}

func (m *mailPart) isMultipart() bool {
	return m.body == nil
}

func (m *mailPart) attachmentFilename() (string, bool) {
	if m.disposition != "attachment" {
		return "", false
	}
	return m.dispositionParams["filename"], true
}

func (m *mailPart) reader() io.Reader {
	if m.isRoot {
		return m.body
	}

	if strings.EqualFold(m.header.Get("Content-Transfer-Encoding"), "base64") {
		return base64.NewDecoder(base64.RawStdEncoding, m.body)
	}

	return m.body
}

func buildPartTree(header mail.Header, body io.Reader) (*mailPart, error) {
	ct := header.Get("Content-Type")

	mt, params, err := mime.ParseMediaType(ct)
	if err != nil {
		return nil, fmt.Errorf("parsing content-type %q: %v", ct, err)
	}

	part := mailPart{
		header:    header,
		mediaType: mt,
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

	part.disposition, part.dispositionParams, err = mime.ParseMediaType(header.Get("Content-Disposition"))
	_ = err // discard
	part.body = new(bytes.Buffer)
	_, err = io.Copy(part.body, body)
	return &part, err
}

func visitParts(mp *mailPart, visit func(*mailPart) error) error {
	debugf("visitParts: %v sub=%v", mp.header.Get("Content-Type"), mp.subparts)

	if mp.isMultipart() {
		for _, p := range mp.subparts {
			if err := visitParts(p, visit); err != nil {
				return err
			}
		}
		return nil
	}

	return visit(mp)
}

func selectParts(mp *mailPart, mediaTypeSpec string, isAttachmentSpec bool) ([]*mailPart, error) {
	parts := []*mailPart{}
	err := visitParts(mp, func(mp *mailPart) error {
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

func checkMatch(h mail.Header, in string, isAddr bool) (bool, error) {
	// TODO: fail if header does not exist
	p := strings.IndexByte(in, ':')
	if p == -1 {
		return false, fmt.Errorf("must be in the form of `header:pattern`: %q", in)
	}
	header, pattern := in[0:p], in[p+1:]

	// TODO: use h.AddressList()
	for _, value := range strings.Split(h.Get(header), ",") {
		value, err := mimeDecoder.DecodeHeader(value)
		if err != nil {
			return false, err
		}
		debugf("test %s: %q against %q", header, value, pattern)
		ok, err := testHeader(value, pattern, isAddr)
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

func testHeader(value, pattern string, isAddr bool) (bool, error) {
	if isAddr {
		addr, err := mail.ParseAddress(value)
		if err != nil {
			return false, fmt.Errorf("parsing address %q: %w", value, err)
		}
		value = addr.Address
	}

	return testPattern(value, pattern)
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

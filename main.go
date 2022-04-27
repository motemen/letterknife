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
	"regexp"
	"strings"

	flag "github.com/spf13/pflag"
	"golang.org/x/text/encoding/ianaindex"
)

func debugf(format string, args ...interface{}) {
	log.Printf("debug: "+format, args...)
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

// --match-address From:...
// --match-header Subject:...
// --select-part text/html
// --print-content
// --print-json // TODO
// --save-file '{{.Subject}}' // TODO
// --list-parts // ???
func main() {
	matchAddress := flag.String("match-address", "", "Filter: address header `header:pattern` eg. \"From:*@example.com\"")
	matchHeader := flag.String("match-header", "", "Filter: header `header:pattern` eg. \"Subject:foobar\"")
	selectPart := flag.String("select-part", "", "Select: part by content type")
	printContent := flag.Bool("print-content", false, "Action: print decoded content") // TODO: make default
	printHeader := flag.String("print-header", "", "Action: print header")
	flag.CommandLine.SortFlags = false
	flag.Parse()

	msg, err := mail.ReadMessage(os.Stdin)
	if err != nil {
		log.Fatal(err)
	}

	pass := true

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

	// TODO: recurse
	var parts []mailPart
	if *selectPart != "" {
		mt, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
		if err != nil {
			log.Fatal(err)
		}
		if strings.HasPrefix(mt, "multipart/") && params["boundary"] != "" {
			mr := multipart.NewReader(msg.Body, params["boundary"])
			parts, err = selectParts(mr, *selectPart)
			if err != nil {
				log.Fatal(err)
			}
		}
	}

	var r io.Reader = msg.Body
	// TODO: do this for parts too
	if strings.EqualFold(msg.Header.Get("Content-Transfer-Encoding"), "base64") {
		r = base64.NewDecoder(base64.RawStdEncoding, r)
	}
	if len(parts) != 0 {
		r = parts[0].body
	}

	if *printContent {
		_, _ = io.Copy(os.Stdout, r)
	}

	if *printHeader != "" {
		if len(parts) != 0 {
			fatalf("cannot print header when selecting subparts")
		}
		s, err := mimeDecoder.DecodeHeader(msg.Header.Get(*printHeader))
		if err != nil {
			fatalf("decoding header %q failed: %v", *printHeader, err)
		}
		fmt.Println(s)
	}
}

type mailPart struct {
	header mail.Header
	body   io.Reader
}

// TODO: selectParts(part, spec) (parts, error)
// errNotMultipart
func selectParts(mr *multipart.Reader, spec string) ([]mailPart, error) {
	var parts []mailPart
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("reading multipart: %v", err)
		}

		ct := p.Header.Get("Content-Type")
		debugf("selectParts: ct=%s", ct)
		mt, params, err := mime.ParseMediaType(ct)
		if err != nil {
			return nil, fmt.Errorf("parsing content-type %q: %v", ct, err)
		}

		ok, err := testPattern(mt, spec)
		if err != nil {
			return nil, err
		}

		if ok {
			var buf bytes.Buffer
			_, err := io.Copy(&buf, p)
			if err != nil {
				fatalf("%v", err)
			}
			parts = append(parts, mailPart{header: mail.Header(p.Header), body: &buf})
		} else if strings.HasPrefix(mt, "multipart/") && params["boundary"] != "" {
			mr := multipart.NewReader(p, params["boundary"])
			subparts, err := selectParts(mr, spec)
			if err != nil {
				return nil, err
			}
			parts = append(parts, subparts...)
		}
	}

	return parts, nil
}

func checkMatch(h mail.Header, in string, isAddr bool) (bool, error) {
	p := strings.IndexByte(in, ':')
	if p == -1 {
		return false, fmt.Errorf("must be in the form of `header:pattern`: %q", in)
	}
	header, pattern := in[0:p], in[p+1:]

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
	if strings.IndexByte(pattern, '*') == -1 {
		return value == pattern, nil
	}

	rx, err := regexpFromPattern(pattern)
	if err != nil {
		return false, err
	}

	return rx.MatchString(value), nil
}

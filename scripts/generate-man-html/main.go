// Generate the HTML manual used by the playground from the manpage's Markdown source.
package main

import (
	"bytes"
	"fmt"
	"os"

	"github.com/yuin/goldmark"
)

func generate(input, output string) error {
	source, err := os.ReadFile(input)
	if err != nil {
		return err
	}
	var html bytes.Buffer
	html.WriteString(`<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>actionlint manual</title></head>
<body>
`)
	if err := goldmark.Convert(source, &html); err != nil {
		return err
	}
	html.WriteString("</body></html>\n")
	return os.WriteFile(output, html.Bytes(), 0o644)
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "Usage: generate-man-html INPUT.md OUTPUT.html")
		os.Exit(1)
	}
	if err := generate(os.Args[1], os.Args[2]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

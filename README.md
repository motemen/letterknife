# letterknife

A tool for querying/manupilating mail messages in command line, useful especially for shell scripts or one liners

## Example

To print HTML from email:

	$ letterknife --html # shortcut for --select-part 'text/html' --print-content

To print email content only if From: header matches `*@example.com`:

	$ letterknife --from '*@example.com' --plain # shortcut for --match-address 'From:*@example.com' --select-part 'text/plain' --print-content

To save attached pdf file

	$ letterknife --save-attachment 'application/pdf' # shortcut for --select-attachment 'application/pdf' --save-file

## Usage

	$ letterknife --help
	Usage of letterknife:
	      --from <pattern>                     Shortcut for --match-address 'From:<pattern>'
	      --subject <pattern>                  Shortcut for --match-header 'Subject:<pattern>'
	      --html                               Shortcut for --select-part text/html
	      --plain                              Shortcut for --select-part text/plain
	      --match-address <header>:<pattern>   Filter: address header <header>:<pattern> eg. "From:*@example.com"
	      --match-header <header>:<pattern>    Filter: header <header>:<pattern> eg. "Subject:foobar"
	      --select-part <content-type>         Select: non-attachment parts by <content-type>
	      --select-attachment <content-type>   Select: attachments by <content-type>
	      --print-content                      Action: print decoded content (default true)
	      --print-header <header>              Action: print <header>
	      --print-raw                          Action: print raw input as-is
	      --save-file                          Action: save parts as files and print their paths
	      --debug                              enable debug logging

// Package classifier: bash_tokenize.go implements a shlex-style tokenizer that
// preserves operator positions so downstream resolvers can derive affected
// paths and recognize protected-release command shapes.
//
// Tokenize handles:
//   - quoted strings (single ' and double ") preserving embedded text
//   - redirects >, >>, <, <<<, <&
//   - pipes |
//   - semicolons ;
//   - && and ||
//   - command substitution $() (including nested) and “ ` “ backticks
//   - balanced parens for grouping / subshells
//
// Tokenize returns an error for unbalanced quotes, redirects, or parens so
// the policy engine can fail closed on malformed shell input (BUG-002 §4b.2(b)
// line 146).
package classifier

import (
	"fmt"
	"strings"
	"unicode"
)

// TokenKind identifies the role of a token within a tokenized bash command.
type TokenKind int

const (
	TkWord      TokenKind = iota
	TkShortFlag           // -i
	TkLongFlag            // --in-place
	TkValue               // positional or flag value
	TkPipe
	TkSemicolon
	TkAnd
	TkOr
	TkRedirect // >, >>, <, <<<, <&
	TkSubshell // $(
	TkBacktick
	TkQuotedString
	TkLParen
	TkRParen
)

// Token is one element returned by Tokenize. Value is the literal text of the
// token (without the surrounding quoting or operator punctuation).
type Token struct {
	Kind  TokenKind
	Value string
}

// String renders the token kind for debug output.
func (k TokenKind) String() string {
	switch k {
	case TkWord:
		return "word"
	case TkShortFlag:
		return "short-flag"
	case TkLongFlag:
		return "long-flag"
	case TkValue:
		return "value"
	case TkPipe:
		return "pipe"
	case TkSemicolon:
		return "semicolon"
	case TkAnd:
		return "and"
	case TkOr:
		return "or"
	case TkRedirect:
		return "redirect"
	case TkSubshell:
		return "subshell"
	case TkBacktick:
		return "backtick"
	case TkQuotedString:
		return "quoted-string"
	case TkLParen:
		return "lparen"
	case TkRParen:
		return "rparen"
	}
	return "unknown"
}

// Tokenize splits a bash command line into tokens while tracking quote and
// paren balance. It returns an error if quotes, redirects, or parens are
// unbalanced (BUG-002 §4b.2(b)). The command is treated as a single line; the
// caller is responsible for splitting pipelines into separate commands before
// resolution when desired.
func Tokenize(command string) ([]Token, error) {
	var tokens []Token
	var current strings.Builder
	currentKind := TkWord
	hasCurrent := false
	inSingle := false
	inDouble := false
	parens := 0
	redirectPending := false

	flush := func() {
		if !hasCurrent {
			return
		}
		tokens = append(tokens, Token{Kind: currentKind, Value: current.String()})
		current.Reset()
		hasCurrent = false
		currentKind = TkWord
	}

	runes := []rune(command)
	for i := 0; i < len(runes); i++ {
		r := runes[i]

		// End-of-input handling for quote contexts.
		if r == 0 {
			break
		}

		switch {
		case inSingle:
			if r == '\'' {
				inSingle = false
				if hasCurrent {
					flush()
				}
				continue
			}
			current.WriteRune(r)
			hasCurrent = true
			currentKind = TkQuotedString

		case inDouble:
			switch r {
			case '"':
				inDouble = false
				if hasCurrent {
					flush()
				}
				continue
			case '\\':
				if i+1 < len(runes) {
					next := runes[i+1]
					// Inside double quotes, only certain escapes are honored.
					// We preserve them literally so downstream resolvers see
					// the resolved text.
					if next == '"' || next == '\\' || next == '$' || next == '`' || next == '\n' {
						i++
						if next == '\n' {
							continue
						}
						current.WriteRune(next)
					} else {
						current.WriteRune(r)
					}
					hasCurrent = true
					currentKind = TkQuotedString
					continue
				}
				return nil, fmt.Errorf("Tokenize: trailing backslash inside double quote")
			case '$':
				// Command substitution $() inside a double-quoted string. The
				// subshell is self-contained: raw-scan finds the matching `)`,
				// the inner command is validated recursively, and we expose
				// the substitution as a single quoted-string token. Unlike
				// the unquoted $() branch below, we MUST NOT increment the
				// outer `parens` counter here — the matching `)` is consumed
				// by `i = j` and the next loop iteration sees it under
				// inDouble, where it falls into the default branch as a
				// literal character (no parens-- to balance). Incrementing
				// parens here would leak and trip the final
				// `parens != 0` check (BUG-007).
				if i+1 < len(runes) && runes[i+1] == '(' {
					flush()
					tokens = append(tokens, Token{Kind: TkSubshell, Value: "$("})
					i++ // consume '('
					// Find matching paren.
					start := i + 1
					depth := 1
					j := start
					for ; j < len(runes); j++ {
						switch runes[j] {
						case '(':
							depth++
						case ')':
							depth--
							if depth == 0 {
								goto subsEnd
							}
						}
					}
					return nil, fmt.Errorf("Tokenize: unbalanced $(  in command substitution")
				subsEnd:
					// Tokenize the inner command for validity, but expose the
					// substitution as a single quoted-string for downstream
					// resolvers (preserving the full structure would leak
					// tokenizer internals into the resolver).
					inner := string(runes[start:j])
					if _, err := Tokenize(inner); err != nil {
						return nil, fmt.Errorf("Tokenize: nested substitution: %w", err)
					}
					tokens = append(tokens, Token{Kind: TkQuotedString, Value: "$(...)"})
					i = j
					continue
				}
				current.WriteRune(r)
				hasCurrent = true
				currentKind = TkQuotedString
			case '`':
				flush()
				tokens = append(tokens, Token{Kind: TkBacktick, Value: "`"})
				// Find matching backtick.
				start := i + 1
				j := start
				for ; j < len(runes); j++ {
					if runes[j] == '`' {
						goto btEnd
					}
				}
				return nil, fmt.Errorf("Tokenize: unbalanced backtick")
			btEnd:
				inner := string(runes[start:j])
				if _, err := Tokenize(inner); err != nil {
					return nil, fmt.Errorf("Tokenize: backtick inner: %w", err)
				}
				tokens = append(tokens, Token{Kind: TkQuotedString, Value: "`...`"})
				i = j
				continue
			default:
				current.WriteRune(r)
				hasCurrent = true
				currentKind = TkQuotedString
			}

		default:
			switch r {
			case ' ', '\t', '\n':
				flush()
			case '\'':
				inSingle = true
				if hasCurrent {
					flush()
				}
			case '"':
				inDouble = true
				if hasCurrent {
					flush()
				}
			case '|':
				flush()
				if i+1 < len(runes) && runes[i+1] == '|' {
					tokens = append(tokens, Token{Kind: TkOr, Value: "||"})
					i++
				} else {
					tokens = append(tokens, Token{Kind: TkPipe, Value: "|"})
				}
			case ';':
				flush()
				tokens = append(tokens, Token{Kind: TkSemicolon, Value: ";"})
			case '&':
				if i+1 < len(runes) && runes[i+1] == '&' {
					flush()
					tokens = append(tokens, Token{Kind: TkAnd, Value: "&&"})
					i++
				} else {
					// Background operator `&` — treat as a separator.
					flush()
					tokens = append(tokens, Token{Kind: TkSemicolon, Value: "&"})
				}
			case '>':
				flush()
				if i+1 < len(runes) && runes[i+1] == '>' {
					tokens = append(tokens, Token{Kind: TkRedirect, Value: ">>"})
					i++
				} else {
					tokens = append(tokens, Token{Kind: TkRedirect, Value: ">"})
				}
				redirectPending = true
			case '<':
				flush()
				// Heredoc << and herestring <<< and fd-redirect <&
				if i+2 < len(runes) && runes[i+1] == '<' && runes[i+2] == '<' {
					tokens = append(tokens, Token{Kind: TkRedirect, Value: "<<<"})
					i += 2
				} else if i+1 < len(runes) && runes[i+1] == '<' {
					tokens = append(tokens, Token{Kind: TkRedirect, Value: "<<"})
					i++
				} else if i+1 < len(runes) && runes[i+1] == '&' {
					tokens = append(tokens, Token{Kind: TkRedirect, Value: "<&"})
					i++
				} else {
					tokens = append(tokens, Token{Kind: TkRedirect, Value: "<"})
				}
				redirectPending = true
			case '(':
				flush()
				parens++
				tokens = append(tokens, Token{Kind: TkLParen, Value: "("})
			case ')':
				flush()
				if parens == 0 {
					return nil, fmt.Errorf("Tokenize: unbalanced closing paren")
				}
				parens--
				tokens = append(tokens, Token{Kind: TkRParen, Value: ")"})
			case '#':
				if !hasCurrent {
					// Rest of line is a comment.
					flush()
					return tokens, nil
				}
				current.WriteRune(r)
				hasCurrent = true
			case '\\':
				if i+1 < len(runes) {
					next := runes[i+1]
					if unicode.IsSpace(next) {
						// Line continuation — drop both.
						i++
						continue
					}
					i++
					current.WriteRune(next)
					hasCurrent = true
				} else {
					return nil, fmt.Errorf("Tokenize: trailing backslash")
				}
			default:
				if !hasCurrent {
					if r == '-' {
						// Possibly a flag.
						if i+1 < len(runes) && runes[i+1] == '-' {
							currentKind = TkLongFlag
							current.WriteRune(r)
							current.WriteRune(runes[i+1])
							i++
							hasCurrent = true
							continue
						}
						currentKind = TkShortFlag
						current.WriteRune(r)
						hasCurrent = true
						continue
					}
					if redirectPending {
						currentKind = TkValue
						redirectPending = false
					} else if r == '$' && i+1 < len(runes) && runes[i+1] == '(' {
						flush()
						tokens = append(tokens, Token{Kind: TkSubshell, Value: "$("})
						i++
						parens++
						start := i + 1
						depth := 1
						j := start
						for ; j < len(runes); j++ {
							switch runes[j] {
							case '(':
								depth++
							case ')':
								depth--
								if depth == 0 {
									goto subsEnd2
								}
							}
						}
						return nil, fmt.Errorf("Tokenize: unbalanced $(  outside quotes")
					subsEnd2:
						inner := string(runes[start:j])
						if _, err := Tokenize(inner); err != nil {
							return nil, fmt.Errorf("Tokenize: nested substitution: %w", err)
						}
						tokens = append(tokens, Token{Kind: TkQuotedString, Value: "$(...)"})
						i = j
						continue
					}
					currentKind = TkWord
				}
				current.WriteRune(r)
				hasCurrent = true
			}
		}
	}

	if inSingle || inDouble {
		return nil, fmt.Errorf("Tokenize: unbalanced quote")
	}
	if parens != 0 {
		return nil, fmt.Errorf("Tokenize: unbalanced parens")
	}

	flush()
	if redirectPending {
		// A trailing redirect with no target is unbalanced.
		return nil, fmt.Errorf("Tokenize: unbalanced redirect (no target)")
	}

	return tokens, nil
}

// WordTokens returns the TkWord/TkShortFlag/TkLongFlag/TkValue tokens only,
// skipping operators. Helpful for resolvers that want a flat argv view.
func WordTokens(tokens []Token) []Token {
	out := make([]Token, 0, len(tokens))
	for _, t := range tokens {
		switch t.Kind {
		case TkWord, TkShortFlag, TkLongFlag, TkValue, TkQuotedString:
			out = append(out, t)
		}
	}
	return out
}

// HasOperator reports whether the token stream contains any non-argv
// operator (pipe, semicolon, &&, ||, redirect, subshell, backtick, parens).
// Used by policy engine to refuse unknown compositions for activated agents.
func HasOperator(tokens []Token) bool {
	for _, t := range tokens {
		switch t.Kind {
		case TkPipe, TkSemicolon, TkAnd, TkOr, TkRedirect, TkSubshell, TkBacktick, TkLParen, TkRParen:
			return true
		}
	}
	return false
}

// FirstWord returns the lowercase literal of the first TkWord token, or "" if
// the command has no word tokens. The program resolver uses this to dispatch
// to a per-family resolver.
func FirstWord(tokens []Token) string {
	for _, t := range tokens {
		if t.Kind == TkWord {
			return strings.ToLower(t.Value)
		}
	}
	return ""
}

//go:build !linux

package setup

// localeSpace is the BSD/macOS C library's [:space:] in a UTF-8 locale, as a
// regexp class body: ASCII \t-\r and every Unicode separator (\p{Z}), the
// no-break spaces included. U+0085 is not included.
const localeSpace = `\t-\r\p{Z}`

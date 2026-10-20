package setup

// localeSpace is glibc's [:space:] in a UTF-8 locale (en_US.UTF-8 and
// C.UTF-8 alike), as a regexp class body: ASCII \t-\r and space plus the
// Unicode separators except the no-break ones (U+00A0, U+2007, U+202F),
// which glibc leaves out. U+0085 is not included.
const localeSpace = `\t-\r \x{1680}\x{2000}-\x{2006}\x{2008}-\x{200a}\x{2028}\x{2029}\x{205f}\x{3000}`

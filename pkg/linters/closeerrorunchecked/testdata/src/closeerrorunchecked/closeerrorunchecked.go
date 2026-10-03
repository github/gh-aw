package closeerrorunchecked

import (
	"bufio"
	"database/sql"
	"io"
	"os"

	"closeerrorunchecked/customerror"
)

// BadExprStmtClose is a bare Close() call with error ignored.
func BadExprStmtClose() {
	f, _ := os.Open("file.txt")
	f.Close() // want `Close\(\) error is explicitly discarded; resource cleanup failures may be silently ignored`
}

// BadBlankAssignClose assigns Close() error to blank identifier.
func BadBlankAssignClose() {
	f, _ := os.Open("file.txt")
	_ = f.Close()   // want `Close\(\) error is explicitly discarded; resource cleanup failures may be silently ignored`
	_ = (f.Close()) // want `Close\(\) error is explicitly discarded; resource cleanup failures may be silently ignored`
}

// BadMultiReturnIgnore ignores Close() error in multi-return assignment.
func BadMultiReturnIgnore() {
	db, _ := sql.Open("driver", "dsn")
	// sql.DB.Close() only returns error (single value), not multi-return
	// This case won't match pattern 2, but is covered by pattern 1 (_ = db.Close())
	_ = db
}

// BadDBClose flags database Close error ignored.
func BadDBClose() error {
	db, _ := sql.Open("driver", "dsn")
	_ = db.Close() // want `Close\(\) error is explicitly discarded; resource cleanup failures may be silently ignored`
	return nil
}

type MultiResultCloser struct{}

func (MultiResultCloser) Close() (int, error) {
	return 0, nil
}

func BadMultiReturnClose() {
	closer := MultiResultCloser{}
	value, _ := closer.Close() // want `Close\(\) error is explicitly discarded; resource cleanup failures may be silently ignored`
	_ = value
	value, _ = (closer.Close()) // want `Close\(\) error is explicitly discarded; resource cleanup failures may be silently ignored`
	_ = value
}

// BadReaderClose flags io.Reader Close error ignored.
func BadReaderClose() error {
	r, _ := os.Open("file.txt")
	var reader io.Reader = r
	closer, ok := reader.(io.Closer)
	if ok {
		_ = closer.Close() // want `Close\(\) error is explicitly discarded; resource cleanup failures may be silently ignored`
	}
	return nil
}

// BadBufferReaderClose flags bufio.Reader Close error ignored.
func BadBufferReaderClose() error {
	f, _ := os.Open("file.txt")
	r := bufio.NewReader(f)
	_ = f.Close() // want `Close\(\) error is explicitly discarded; resource cleanup failures may be silently ignored`
	_ = r
	return nil
}

// GoodDeferClose uses defer, which is the best practice — not flagged.
func GoodDeferClose() error {
	f, _ := os.Open("file.txt")
	defer f.Close()
	return nil
}

// GoodErrorChecked checks the Close() error — not flagged.
func GoodErrorChecked() error {
	f, _ := os.Open("file.txt")
	if err := f.Close(); err != nil {
		return err
	}
	return nil
}

// GoodErrorAssignedWithCheck assigns Close() error and checks it — not flagged.
func GoodErrorAssignedWithCheck() error {
	f, _ := os.Open("file.txt")
	err := f.Close()
	if err != nil {
		return err
	}
	return nil
}

// GoodNoAssignment doesn't call Close() at all — not flagged.
func GoodNoAssignment() error {
	f, _ := os.Open("file.txt")
	_ = f
	return nil
}

// GoodInlineErrorCheck checks error inline — not flagged.
func GoodInlineErrorCheck() error {
	f, _ := os.Open("file.txt")
	if f.Close() != nil {
		return nil
	}
	return nil
}

// CustomCloser is a custom type that implements io.Closer.
type CustomCloser struct {
	closed bool
}

func (c *CustomCloser) Close() error {
	c.closed = true
	return nil
}

// BadCustomCloserClose flags custom type Close error ignored.
func BadCustomCloserClose() {
	c := &CustomCloser{}
	_ = c.Close() // want `Close\(\) error is explicitly discarded; resource cleanup failures may be silently ignored`
}

// GoodCustomCloserDefer defers custom type Close — not flagged.
func GoodCustomCloserDefer() {
	c := &CustomCloser{}
	defer c.Close()
}

// NonCloser is a type with a Close() method that doesn't return error.
type NonCloser struct {
}

func (nc *NonCloser) Close() {
	// returns void, not error
}

type ArgumentCloser struct{}

func (ArgumentCloser) Close(bool) error {
	return nil
}

func GoodCloseWithArgument() {
	closer := ArgumentCloser{}
	closer.Close(true)
}

func GoodCloseReturningNamedError() {
	closer := customerror.Closer{}
	closer.Close()
}

// GoodNonCloserNoError doesn't flag Close() methods without error return — not flagged.
func GoodNonCloserNoError() {
	nc := &NonCloser{}
	nc.Close()
	_ = nc
}

// SuppressedWithNolint uses nolint comment to suppress the check — not flagged.
func SuppressedWithNolint() error {
	f, _ := os.Open("file.txt")
	_ = f.Close() //nolint:closeerrorunchecked
	return nil
}

// BadMultiAssignmentClose flags Close error ignored in expression.
func BadMultiAssignmentClose() {
	f, _ := os.Open("file.txt")
	_ = f.Close() // want `Close\(\) error is explicitly discarded; resource cleanup failures may be silently ignored`
}

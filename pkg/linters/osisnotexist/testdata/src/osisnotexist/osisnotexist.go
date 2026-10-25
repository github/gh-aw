package osisnotexist

import (
	"errors"
	"fmt"
	"io/fs"
	operating "os"
)

func predicates(err error) {
	_ = operating.IsNotExist(err)   // want `os.IsNotExist.*errors.Is\(err, fs.ErrNotExist\)`
	_ = operating.IsExist(err)      // want `os.IsExist.*errors.Is\(err, fs.ErrExist\)`
	_ = operating.IsPermission(err) // want `os.IsPermission.*errors.Is\(err, fs.ErrPermission\)`
	_ = operating.IsTimeout(err)    // want `os.IsTimeout.*errors.As.*Timeout\(\).*only covers deadline errors`
	_ = (operating.IsNotExist)(err) // want `os.IsNotExist.*errors.Is`
	operating.IsExist(err)          // want `os.IsExist.*errors.Is`
	predicate := operating.IsNotExist
	_ = predicate(err)                                       // want `os.IsNotExist.*errors.Is`
	_ = operating.IsNotExist(fmt.Errorf("wrapped: %w", err)) // want `os.IsNotExist.*errors.Is`
}

func good(err error) {
	_ = errors.Is(err, fs.ErrNotExist)
	_ = errors.Is(err, fs.ErrExist)
	_ = errors.Is(err, fs.ErrPermission)
	_ = errors.Is(err, operating.ErrDeadlineExceeded)
	var timeout interface{ Timeout() bool }
	_ = errors.As(err, &timeout) && timeout.Timeout()
	_ = operating.IsPathSeparator('/')
}

func suppressed(err error) {
	_ = operating.IsNotExist(err) //nolint:osisnotexist
	//nolint:osisnotexist
	_ = operating.IsTimeout(err)
}

type fakeOS struct{}

func (fakeOS) IsNotExist(error) bool { return false }

func shadowed(err error) {
	os := fakeOS{}
	_ = os.IsNotExist(err)
	predicate := operating.IsExist
	predicate = func(error) bool { return false }
	_ = predicate(err)
}

func directErrors(path string) {
	if _, err := operating.Stat(path); operating.IsNotExist(err) {
	}
	if _, err := operating.Stat(fmt.Sprint(path)); !operating.IsNotExist(err) {
	}
	if _, err := operating.Stat(path); err != nil {
		if operating.IsNotExist(err) {
		}
	}
	_, err := operating.ReadDir(path)
	if err != nil {
		if operating.IsNotExist(err) {
		}
	}
}

func directPredicates(path string) {
	if err := operating.Mkdir(path, 0700); operating.IsExist(err) {
	}
	if _, err := operating.Open(path); operating.IsPermission(err) {
	}
	if _, err := operating.ReadFile(path); operating.IsTimeout(err) {
	}
	if _, err := (operating.Lstat)(path); (operating.IsNotExist)((err)) {
	}
}

func directAssignment(path string) bool {
	err := operating.Remove(path)
	return operating.IsPermission(err)
}

func directAssignmentInReturnCall(path string) bool {
	err := operating.Remove(path)
	return bool(operating.IsNotExist(err))
}

func directAssignmentInSinkCall(path string) {
	err := operating.Remove(path)
	observe(operating.IsNotExist(err))
}

func directAssignmentAfterEarlierArgument(path string, earlier func() bool) {
	err := operating.Remove(path)
	observeTwo(earlier(), operating.IsNotExist(err)) // want `os.IsNotExist.*errors.Is`
}

func observe(bool) {}

func observeTwo(bool, bool) {}

func unknownOrigin(path string, source fs.FS, unknown func(string) (operating.FileInfo, error)) {
	if _, err := unknown(path); operating.IsNotExist(err) { // want `os.IsNotExist.*errors.Is`
	}
	stat := operating.Stat
	if _, err := stat(path); operating.IsNotExist(err) { // want `os.IsNotExist.*errors.Is`
	}
	if err := operating.NewSyscallError("stat", errors.New("unknown")); operating.IsTimeout(err) { // want `os.IsTimeout.*errors.As`
	}
	if err := operating.CopyFS(path, source); operating.IsNotExist(err) { // want `os.IsNotExist.*errors.Is`
	}
}

func modifiedErrors(path string, incoming error) {
	if _, err := operating.Stat(path); err != nil {
		err = fmt.Errorf("wrapped: %w", err)
		_ = operating.IsNotExist(err) // want `os.IsNotExist.*errors.Is`
	}
	if _, err := operating.Stat(path); err != nil {
		err = errors.Join(err, incoming)
		_ = operating.IsExist(err) // want `os.IsExist.*errors.Is`
	}
	if _, err := operating.Stat(path); err != nil {
		err := incoming
		_ = operating.IsPermission(err) // want `os.IsPermission.*errors.Is`
	}
	_, err := operating.Stat(path)
	alias := err
	_ = operating.IsTimeout(alias) // want `os.IsTimeout.*errors.As`
}

func indirectUse(path string, unknown func() error) {
	if _, err := operating.Stat(path); err != nil {
		_ = operating.IsNotExist(fmt.Errorf("wrapped: %w", err)) // want `os.IsNotExist.*errors.Is`
		_ = operating.IsNotExist(errors.Join(err, unknown()))    // want `os.IsNotExist.*errors.Is`
	}
	_, err := operating.Stat(path)
	_ = unknown()
	_ = operating.IsNotExist(err) // want `os.IsNotExist.*errors.Is`
}

func mutate(err *error) bool {
	*err = fmt.Errorf("wrapped: %w", *err)
	return true
}

func sideEffects(path string) {
	if _, err := operating.Stat(path); mutate(&err) && operating.IsNotExist(err) { // want `os.IsNotExist.*errors.Is`
	}
	_, err := operating.Stat(path)
	if mutate(&err) {
		_ = operating.IsPermission(err) // want `os.IsPermission.*errors.Is`
	}
}

func deferredAndRepeated(path string, incoming error) {
	if _, err := operating.Stat(path); err != nil {
		check := func() bool {
			return operating.IsNotExist(err) // want `os.IsNotExist.*errors.Is`
		}
		err = incoming
		_ = check()
	}
	if _, err := operating.Stat(path); err != nil {
		for operating.IsTimeout(err) { // want `os.IsTimeout.*errors.As`
			err = incoming
		}
	}
}

func reusedBinding(path string, err error) {
	_, err = operating.Stat(path)
	_ = operating.IsNotExist(err) // want `os.IsNotExist.*errors.Is`
	info, err := operating.Stat(path)
	_ = operating.IsPermission(err) // want `os.IsPermission.*errors.Is`
	_ = info
}

func shadowedSource(path string) {
	os := struct {
		Stat func(string) (operating.FileInfo, error)
	}{operating.Stat}
	if _, err := os.Stat(path); operating.IsNotExist(err) { // want `os.IsNotExist.*errors.Is`
	}
}

func escapedErrors(path string) {
	if _, err := operating.Stat(path); err != nil {
		pointer := &err
		*pointer = fmt.Errorf("wrapped: %w", *pointer)
		_ = operating.IsNotExist(err) // want `os.IsNotExist.*errors.Is`
	}
	if _, err := operating.Stat(path); err != nil {
		if func() bool {
			err = fmt.Errorf("wrapped: %w", err)
			return true
		}() && operating.IsTimeout(err) { // want `os.IsTimeout.*errors.As`
		}
	}
}

func directElseIf(path string) bool {
	if _, err := operating.Stat(path); err == nil {
		return operating.Remove(path) == nil
	} else if !operating.IsNotExist(err) {
		return true
	}
	return false
}

func directElse(path string) {
	if _, err := operating.Stat(path); err == nil {
		mutate(&err)
	} else {
		_ = operating.IsTimeout(err)
	}
}

func modifiedElseConditions(path string) {
	if _, err := operating.Stat(path); mutate(&err) {
	} else if operating.IsNotExist(err) { // want `os.IsNotExist.*errors.Is`
	}
	if _, err := operating.Stat(path); err == nil {
	} else if mutate(&err) {
	} else if operating.IsPermission(err) { // want `os.IsPermission.*errors.Is`
	}
	if _, err := operating.Stat(path); func() bool {
		err = fmt.Errorf("wrapped: %w", err)
		return false
	}() {
	} else if operating.IsExist(err) { // want `os.IsExist.*errors.Is`
	}
}

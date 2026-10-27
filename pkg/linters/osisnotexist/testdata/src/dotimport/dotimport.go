package dotimport

import . "os"

func predicates(err error) {
	_ = IsNotExist(err)   // want `os.IsNotExist.*errors.Is`
	_ = (IsExist)(err)    // want `os.IsExist.*errors.Is`
	_ = IsPermission(err) // want `os.IsPermission.*errors.Is`
	_ = IsTimeout(err)    // want `os.IsTimeout.*errors.As`
}

func directErrors(path string) {
	if _, err := (Stat)(path); (IsNotExist)((err)) {
	}
	if err := Mkdir(path, 0700); IsExist(err) {
	}
	if _, err := Open(path); err != nil {
		if IsPermission(err) {
		}
	}
	if _, err := Stat(path); (err) != (nil) {
		_ = (IsNotExist)((err))
	}
	_, err := ReadDir(path)
	if err != nil {
		if IsTimeout(err) {
		}
	}
}

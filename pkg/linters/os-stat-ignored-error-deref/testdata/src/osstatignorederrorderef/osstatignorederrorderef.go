package osstatignorederrorderef

import "os"

func badStat(p string) bool {
	fi, _ := os.Stat(p)
	return fi.IsDir() // want `fi from os.Stat is used without a nil check`
}

func badLstat(p string) string {
	fi, _ := os.Lstat(p)
	return fi.Name() // want `fi from os.Lstat is used without a nil check`
}

func badParen(p string) int64 {
	var fi os.FileInfo
	fi, _ = (os.Stat)(p)
	return (fi).Size() // want `fi from os.Stat is used without a nil check`
}

func badIfInit(p string) bool {
	if fi, _ := os.Stat(p); fi.IsDir() { // want `fi from os.Stat is used without a nil check`
		return true
	}
	return false
}

func goodErrChecked(p string) bool {
	fi, err := os.Stat(p)
	if err != nil {
		return false
	}
	return fi.IsDir()
}

func goodNilChecked(p string) bool {
	fi, _ := os.Stat(p)
	if fi == nil {
		return false
	}
	return fi.IsDir()
}

func goodNilAnd(p string) bool {
	fi, _ := os.Stat(p)
	return fi != nil && fi.IsDir()
}

func goodNoUse(p string) bool {
	fi, _ := os.Stat(p)
	return fi != nil
}

type fake struct{}

func (fake) Stat(string) (*int, error) { return nil, nil }

func goodOtherStat(p string) int {
	var f fake
	v, _ := f.Stat(p)
	return *v
}

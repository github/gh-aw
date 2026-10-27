package errorssentinelcompare

import (
	"context"
	"errors"
	"io"
	"os"
)

var ErrLocal = errors.New("local")

func bad(err error) bool {
	return err == io.ErrUnexpectedEOF // want `use errors\.Is\(err, io\.ErrUnexpectedEOF\) instead of comparing with ==`
}

func badNeq(err error) bool {
	return err != os.ErrNotExist // want `use errors\.Is\(err, os\.ErrNotExist\) instead of comparing with !=`
}

func badLocal(err error) bool {
	return ErrLocal == err // want `use errors\.Is\(err, ErrLocal\) instead of comparing with ==`
}

func badParen(err error) bool {
	return (err) == (ErrLocal) // want `use errors\.Is\(err, ErrLocal\) instead of comparing with ==`
}

func goodIs(err error) bool {
	return errors.Is(err, ErrLocal)
}

func goodEOF(err error) bool {
	return err == io.EOF
}

func goodNil(err error) bool {
	return err == nil
}

func goodSentinels() bool {
	return ErrLocal == io.ErrUnexpectedEOF // want `use errors\.Is`
}

func goodLocalVar(a, b error) bool {
	return a == b
}

func goodCtx(ctx context.Context) bool {
	return ctx.Err() == context.Canceled // want `use errors\.Is`
}

type myErr struct{}

func (myErr) Error() string { return "x" }

func (myErr) Is(target error) bool {
	return target == ErrLocal
}

func suppressed(err error) bool {
	return err == ErrLocal //nolint:errorssentinelcompare
}

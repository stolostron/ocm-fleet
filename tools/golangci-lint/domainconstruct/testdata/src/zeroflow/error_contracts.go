package zeroflow

import (
	"context"
	"errors"
	"fmt"
)

var ErrMissing = errors.New("missing")
var ErrAlias = ErrMissing

var mutableError = errors.New("initial")

func ClearMutableError() { mutableError = nil }

var escapedError = errors.New("initial")

func ErrorAddress() *error { return &escapedError }

type wrappedError struct{ cause error }

func (e *wrappedError) Error() string { return e.cause.Error() }

func Terminal(err error) error {
	if err == nil {
		return nil
	}
	return &wrappedError{cause: err}
}

func ForwardTerminal(err error) error { return Terminal(err) }

func AlwaysWrap(err error) error { return fmt.Errorf("wrapped: %w", err) }

func SentinelFailure() (Ref, error) { return Ref{}, ErrMissing }

func AliasedSentinelFailure() (Ref, error) { return Ref{}, ErrAlias }

func MutableFailure() (Ref, error) {
	return Ref{}, mutableError // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]Ref\""
}

func EscapedFailure() (Ref, error) {
	return Ref{}, escapedError // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]Ref\""
}

func WrappedFailure() (Ref, error) { return Ref{}, Terminal(errors.New("failure")) }

func GuardedWrappedFailure(err error) (Ref, error) {
	if err != nil {
		return Ref{}, ForwardTerminal(err)
	}
	return NewRef("valid")
}

func MaybeWrappedFailure(err error) (Ref, error) {
	return Ref{}, Terminal(err) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]Ref\""
}

func NilWrappedFailure() (Ref, error) {
	return Ref{}, Terminal(nil) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]Ref\""
}

func AlwaysWrappedFailure(err error) (Ref, error) { return Ref{}, AlwaysWrap(err) }

func ForwardWrappedResult(raw string) (ID, error) {
	id, err := NewID(raw)
	return id, Terminal(err)
}

func JoinedFailure() (Ref, error) { return Ref{}, errors.Join(nil, ErrMissing) }

func NilJoinedFailure() (Ref, error) {
	return Ref{}, errors.Join(nil, nil) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]Ref\""
}

func CanceledFailure(ctx context.Context) (Ref, error) {
	<-ctx.Done()
	return Ref{}, ctx.Err()
}

func SelectCanceledFailure(ctx context.Context) (Ref, error) {
	select {
	case <-ctx.Done():
		return Ref{}, ctx.Err()
	default:
		return NewRef("valid")
	}
}

func UncanceledFailure(ctx context.Context) (Ref, error) {
	return Ref{}, ctx.Err() // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]Ref\""
}

func ClearError(err *error) { *err = nil }

func EscapingWrapper(err error) error {
	if err == nil {
		return nil
	}
	ClearError(&err)
	return err
}

func CapturingWrapper(err error) error {
	if err == nil {
		return nil
	}
	func() { err = nil }()
	return err
}

func UnsafeWrappers(err error) (Ref, error) {
	if err != nil {
		return Ref{}, EscapingWrapper(err) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]Ref\""
	}
	return Ref{}, CapturingWrapper(ErrMissing) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]Ref\""
}

func JoinedMutatedSlice() (Ref, error) {
	errorsToJoin := []error{ErrMissing}
	clear(errorsToJoin)
	return Ref{}, errors.Join(errorsToJoin...) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]Ref\""
}

func JoinedFilledSlice() (Ref, error) {
	errorsToJoin := make([]error, 1)
	errorsToJoin[0] = ErrMissing
	return Ref{}, errors.Join(errorsToJoin...)
}

func ContextCause(ctx context.Context) (Ref, error) {
	<-ctx.Done()
	return Ref{}, context.Cause(ctx)
}

func OtherContext(first, second context.Context) (Ref, error) {
	<-first.Done()
	return Ref{}, second.Err() // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]Ref\""
}

func JoinWrapper(err error) error { return errors.Join(nil, err) }

func JoinWrappedFailure() (Ref, error) { return Ref{}, JoinWrapper(ErrMissing) }

func ClearErrors(values []error) { clear(values) }

func JoinedEscapedSlice() (Ref, error) {
	errorsToJoin := []error{ErrMissing}
	ClearErrors(errorsToJoin)
	return Ref{}, errors.Join(errorsToJoin...) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]Ref\""
}

func JoinedPartialClear() (Ref, error) {
	errorsToJoin := []error{ErrMissing, ErrMissing}
	clear(errorsToJoin[:1])
	return Ref{}, errors.Join(errorsToJoin...)
}

type ErrorWrapper struct{}

func (*ErrorWrapper) Wrap(err error) error {
	if err == nil {
		return nil
	}
	return &wrappedError{cause: err}
}

func MethodExpressionWrapper(err error) error {
	return (*ErrorWrapper).Wrap(new(ErrorWrapper), err)
}

func NilMethodExpressionFailure() (Ref, error) {
	return Ref{}, MethodExpressionWrapper(nil) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]Ref\""
}

type ErrorSlots struct{ err error }

func MutateOtherError(first, second *ErrorSlots) error {
	first.err = errors.New("first")
	return second.err
}

func OtherErrorFailure(first, second *ErrorSlots) (Ref, error) {
	return Ref{}, MutateOtherError(first, second) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]Ref\""
}

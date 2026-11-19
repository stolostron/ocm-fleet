package contractbounds

import "errors"

func Simple() error { return errors.New("failure") }

func Nested(flag func() bool) error {
	if flag() {
	}
	if flag() {
	}
	if flag() {
	}
	if flag() {
	}
	if flag() {
	}
	{
		if flag() {
		}
		if flag() {
		}
		if flag() {
		}
		if flag() {
		}
		if flag() {
		}
		{
			if flag() {
			}
			if flag() {
			}
			if flag() {
			}
			if flag() {
			}
			if flag() {
			}
			return errors.New("failure")
		}
	}
}

func NestedGeneric[T any](flag func() bool, value T) T {
	if flag() {
	}
	if flag() {
	}
	if flag() {
	}
	if flag() {
	}
	if flag() {
	}
	{
		if flag() {
		}
		if flag() {
		}
		if flag() {
		}
		if flag() {
		}
		if flag() {
		}
		{
			if flag() {
			}
			if flag() {
			}
			if flag() {
			}
			if flag() {
			}
			if flag() {
			}
			return value
		}
	}
}

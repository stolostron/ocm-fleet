package presencepolicy

//domain:presence
func NoResults() {} // want "domain:presence requires"

//domain:presence
func MissingFlag() int { return 1 } // want "domain:presence requires"

//domain:presence
func WrongFlag() (int, string) { return 1, "present" } // want "domain:presence requires"

//domain:presence
func WrongError() (int, bool, string) { return 1, true, "" } // want "domain:presence requires"

//domain:presence
func MultiplePayloads() (int, string, bool) { return 1, "present", true } // want "domain:presence requires"

//domain:presence
func TooManyResults() (int, bool, bool, error) { return 1, true, true, nil } // want "domain:presence requires"

//domain:presence
func ErrorPayload() (error, bool) { return nil, false } // want "domain:presence requires"

//domain:presence value=0
func Arguments() (int, bool) { return 1, true } // want "use domain:presence once, without arguments"

//domain:presence
//domain:presence
func Duplicate() (int, bool) { return 1, true } // want "use domain:presence once, without arguments"

type Found bool

//domain:presence
func NamedFlag() (int, Found) { return 1, true } // want "domain:presence requires"

type BoolAlias = bool

//domain:presence
func AliasFlag() (int, BoolAlias) { return 1, true }

type Provider interface {
	//domain:presence
	Wrong() (int, string) // want "domain:presence requires"
	//domain:presence
	Valid() (int, bool)
}

package contractchain

import "errors"

var IndependentError = errors.New("independent")

func Independent[T ~string](raw string) T            { return T(raw) }
func AlsoIndependent[T ~string](raw string) T        { return Independent[T](raw) }
func DependencyOnUnfinished[T ~string](raw string) T { return First[T](raw) }

func First[T ~string](raw string) T  { return next0[T](raw) }
func next0[T ~string](raw string) T  { return next1[T](raw) }
func next1[T ~string](raw string) T  { return next2[T](raw) }
func next2[T ~string](raw string) T  { return next3[T](raw) }
func next3[T ~string](raw string) T  { return next4[T](raw) }
func next4[T ~string](raw string) T  { return next5[T](raw) }
func next5[T ~string](raw string) T  { return next6[T](raw) }
func next6[T ~string](raw string) T  { return next7[T](raw) }
func next7[T ~string](raw string) T  { return next8[T](raw) }
func next8[T ~string](raw string) T  { return next9[T](raw) }
func next9[T ~string](raw string) T  { return next10[T](raw) }
func next10[T ~string](raw string) T { return next11[T](raw) }
func next11[T ~string](raw string) T { return next12[T](raw) }
func next12[T ~string](raw string) T { return next13[T](raw) }
func next13[T ~string](raw string) T { return next14[T](raw) }
func next14[T ~string](raw string) T { return next15[T](raw) }
func next15[T ~string](raw string) T { return next16[T](raw) }
func next16[T ~string](raw string) T { return next17[T](raw) }
func next17[T ~string](raw string) T { return T(raw) }

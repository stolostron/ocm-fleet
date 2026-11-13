package flowhelpers

func Inspect(raw *string) bool { return *raw == "" }

func AssignEmpty(raw *string) { *raw = "" }

func AssignNonempty(raw *string) { *raw = "nonempty" }

func Convert[T ~string](raw string) T { return T(raw) }

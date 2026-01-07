package impl

// Properties collects properties of an ImageSource that are constant throughout its lifetime
// (but might differ across instances).
type Properties struct {
	// HasThreadSafeGetBlob indicates whether GetBlob can be executed concurrently.
	HasThreadSafeGetBlob bool
}

// PropertyMethodsInitialize implements parts of private.ImageSource corresponding to Properties.
type PropertyMethodsInitialize struct {
	// We need two separate structs, PropertyMethodsInitialize and Properties, because Go prohibits fields and methods with the same name.

	vals Properties
}

// PropertyMethods creates an PropertyMethodsInitialize for vals.
func PropertyMethods(vals Properties) PropertyMethodsInitialize {
	return PropertyMethodsInitialize{
		vals: vals,
	}
}

// HasThreadSafeGetBlob indicates whether GetBlob can be executed concurrently.
func (o PropertyMethodsInitialize) HasThreadSafeGetBlob() bool {
	return o.vals.HasThreadSafeGetBlob
}

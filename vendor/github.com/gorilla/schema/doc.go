// Copyright 2012 The Gorilla Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package gorilla/schema fills a struct with form values.

The basic usage is really simple. Given this struct:

	type Person struct {
		Name  string
		Phone string
	}

...we can fill it passing a map to the Decode() function:

	values := map[string][]string{
		"Name":  {"John"},
		"Phone": {"999-999-999"},
	}
	person := new(Person)
	decoder := schema.NewDecoder()
	decoder.Decode(person, values)

This is just a simple example and it doesn't make a lot of sense to create
the map manually. Typically it will come from a http.Request object and
will be of type url.Values, http.Request.Form, or http.Request.MultipartForm:

	func MyHandler(w http.ResponseWriter, r *http.Request) {
		err := r.ParseForm()

		if err != nil {
			// Handle error
		}

		decoder := schema.NewDecoder()
		// r.PostForm is a map of our POST form values
		err := decoder.Decode(person, r.PostForm)

		if err != nil {
			// Handle error
		}

		// Do something with person.Name or person.Phone
	}

Note: it is a good idea to set a Decoder instance as a package global,
because it caches meta-data about structs, and an instance can be shared safely:

	var decoder = schema.NewDecoder()

To define custom names for fields, use a struct tag "schema". To not populate
certain fields, use a dash for the name and it will be ignored:

	type Person struct {
		Name  string `schema:"name"`  // custom name
		Phone string `schema:"phone"` // custom name
		Admin bool   `schema:"-"`     // this field is never set
	}

The supported field types in the destination struct are:

	* bool
	* float variants (float32, float64)
	* int variants (int, int8, int16, int32, int64)
	* string
	* uint variants (uint, uint8, uint16, uint32, uint64)
	* struct
	* a pointer to one of the above types
	* a slice or a pointer to a slice of one of the above types

Non-supported types are simply ignored, however custom types can be registered
to be converted.

To fill nested structs, keys must use a dotted notation as the "path" for the
field. So for example, to fill the struct Person below:

	type Phone struct {
		Label  string
		Number string
	}

	type Person struct {
		Name  string
		Phone Phone
	}

...the source map must have the keys "Name", "Phone.Label" and "Phone.Number".
This means that an HTML form to fill a Person struct must look like this:

	<form>
		<input type="text" name="Name">
		<input type="text" name="Phone.Label">
		<input type="text" name="Phone.Number">
	</form>

Single values are filled using the first value for a key from the source map.
Slices are filled using all values for a key from the source map. So to fill
a Person with multiple Phone values, like:

	type Person struct {
		Name   string
		Phones []Phone
	}

...an HTML form that accepts three Phone values would look like this:

	<form>
		<input type="text" name="Name">
		<input type="text" name="Phones.0.Label">
		<input type="text" name="Phones.0.Number">
		<input type="text" name="Phones.1.Label">
		<input type="text" name="Phones.1.Number">
		<input type="text" name="Phones.2.Label">
		<input type="text" name="Phones.2.Number">
	</form>

Notice that only for slices of structs the slice index is required.
This is needed for disambiguation: if the nested struct also had a slice
field, we could not translate multiple values to it if we did not use an
index for the parent struct.

There's also the possibility to create a custom type that implements the
TextUnmarshaler interface, and in this case there's no need to register
a converter, like:

	type Person struct {
	  Emails []Email
	}

	type Email struct {
	  *mail.Address
	}

	func (e *Email) UnmarshalText(text []byte) (err error) {
		e.Address, err = mail.ParseAddress(string(text))
		return
	}

...an HTML form that accepts three Email values would look like this:

	<form>
		<input type="email" name="Emails.0">
		<input type="email" name="Emails.1">
		<input type="email" name="Emails.2">
	</form>
*/
package schema

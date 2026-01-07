# gorilla/schema

![testing](https://github.com/gorilla/schema/actions/workflows/test.yml/badge.svg)
[![codecov](https://codecov.io/github/gorilla/schema/branch/main/graph/badge.svg)](https://codecov.io/github/gorilla/schema)
[![godoc](https://godoc.org/github.com/gorilla/schema?status.svg)](https://godoc.org/github.com/gorilla/schema)
[![sourcegraph](https://sourcegraph.com/github.com/gorilla/schema/-/badge.svg)](https://sourcegraph.com/github.com/gorilla/schema?badge)


![Gorilla Logo](https://github.com/gorilla/.github/assets/53367916/d92caabf-98e0-473e-bfbf-ab554ba435e5)

Package gorilla/schema converts structs to and from form values.

## Example

Here's a quick example: we parse POST form values and then decode them into a struct:

```go
// Set a Decoder instance as a package global, because it caches
// meta-data about structs, and an instance can be shared safely.
var decoder = schema.NewDecoder()

type Person struct {
    Name  string
    Phone string
}

func MyHandler(w http.ResponseWriter, r *http.Request) {
    err := r.ParseForm()
    if err != nil {
        // Handle error
    }

    var person Person

    // r.PostForm is a map of our POST form values
    err = decoder.Decode(&person, r.PostForm)
    if err != nil {
        // Handle error
    }

    // Do something with person.Name or person.Phone
}
```

Conversely, contents of a struct can be encoded into form values. Here's a variant of the previous example using the Encoder:

```go
var encoder = schema.NewEncoder()

func MyHttpRequest() {
    person := Person{"Jane Doe", "555-5555"}
    form := url.Values{}

    err := encoder.Encode(person, form)

    if err != nil {
        // Handle error
    }

    // Use form values, for example, with an http client
    client := new(http.Client)
    res, err := client.PostForm("http://my-api.test", form)
}

```

To define custom names for fields, use a struct tag "schema". To not populate certain fields, use a dash for the name and it will be ignored:

```go
type Person struct {
    Name  string `schema:"name,required"`  // custom name, must be supplied
    Phone string `schema:"phone"`          // custom name
    Admin bool   `schema:"-"`              // this field is never set
}
```

The supported field types in the struct are:

* bool
* float variants (float32, float64)
* int variants (int, int8, int16, int32, int64)
* string
* uint variants (uint, uint8, uint16, uint32, uint64)
* struct
* a pointer to one of the above types
* a slice or a pointer to a slice of one of the above types

Unsupported types are simply ignored, however custom types can be registered to be converted.

## Setting Defaults

It is possible to set default values when encoding/decoding by using the `default` tag option. The value of `default` is applied when a field has a zero value, a pointer has a nil value, or a slice is empty.

```go
type Person struct {
    Phone string `schema:"phone,default:+123456"`          // custom name
    Age int     `schema:"age,default:21"`
	Admin bool    `schema:"admin,default:false"`
	Balance float64 `schema:"balance,default:10.0"`
    Friends []string `schema:friends,default:john|bob`
}
```

The `default` tag option is supported for the following types:

* bool
* float variants (float32, float64)
* int variants (int, int8, int16, int32, int64)
* uint variants (uint, uint8, uint16, uint32, uint64)
* string
* a slice of the above types. As shown in the example above, `|` should be used to separate between slice items. 
* a pointer to one of the above types (pointer to slice and slice of pointers are not supported).

> [!NOTE]  
> Because primitive types like int, float, bool, unint and their variants have their default (or zero) values set by Golang, it is not possible to distinguish them from a provided value when decoding/encoding form values. In this case, the value provided by the `default` option tag will be always applied. For example, let's assume that the value submitted in the form for `balance` is `0.0` then the default of `10.0` will be applied, even if `0.0` is part of the form data for the `balance` field. In such cases, it is highly recommended to use pointers to allow schema to distinguish between when a form field has no provided value and when a form has a value equal to the corresponding default set by Golang for a particular type. If the type of the `Balance` field above is changed to `*float64`, then the zero value would be `nil`. In this case, if the form data value for `balance` is `0.0`, then the default will not be applied.

## License

BSD licensed. See the LICENSE file for details.

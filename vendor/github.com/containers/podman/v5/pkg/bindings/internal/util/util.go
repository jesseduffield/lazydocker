package util

import (
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"strings"

	jsoniter "github.com/json-iterator/go"
)

func IsSimpleType(f reflect.Value) bool {
	if _, ok := f.Interface().(fmt.Stringer); ok {
		return true
	}

	switch f.Kind() {
	case reflect.Bool, reflect.Int, reflect.Int64, reflect.Uint, reflect.Uint64, reflect.String:
		return true
	}

	return false
}

func SimpleTypeToParam(f reflect.Value) string {
	if s, ok := f.Interface().(fmt.Stringer); ok {
		return s.String()
	}

	switch f.Kind() {
	case reflect.Bool:
		return strconv.FormatBool(f.Bool())
	case reflect.Int, reflect.Int64:
		// f.Int() is always an int64
		return strconv.FormatInt(f.Int(), 10)
	case reflect.Uint, reflect.Uint64:
		// f.Uint() is always an uint64
		return strconv.FormatUint(f.Uint(), 10)
	case reflect.String:
		return f.String()
	}

	panic("the input parameter is not a simple type")
}

func Changed(o any, fieldName string) bool {
	r := reflect.ValueOf(o)
	value := reflect.Indirect(r).FieldByName(fieldName)
	return !value.IsNil()
}

func ToParams(o any) (url.Values, error) {
	params := url.Values{}
	if o == nil || reflect.ValueOf(o).IsNil() {
		return params, nil
	}
	json := jsoniter.ConfigCompatibleWithStandardLibrary
	s := reflect.ValueOf(o)
	if reflect.Ptr == s.Kind() {
		s = s.Elem()
	}
	sType := s.Type()
	for i := 0; i < s.NumField(); i++ {
		fieldName := sType.Field(i).Name
		if !Changed(o, fieldName) {
			continue
		}
		fieldName = strings.ToLower(fieldName)
		f := s.Field(i)
		if reflect.Ptr == f.Kind() {
			f = f.Elem()
		}
		paramName := fieldName
		if pn, ok := sType.Field(i).Tag.Lookup("schema"); ok {
			if pn == "-" {
				continue
			}
			paramName = pn
		}
		switch {
		case IsSimpleType(f):
			params.Set(paramName, SimpleTypeToParam(f))
		case f.Kind() == reflect.Slice:
			for i := 0; i < f.Len(); i++ {
				elem := f.Index(i)
				if IsSimpleType(elem) {
					params.Add(paramName, SimpleTypeToParam(elem))
				} else {
					return nil, errors.New("slices must contain only simple types")
				}
			}
		case f.Kind() == reflect.Map:
			lowerCaseKeys := make(map[string]any)
			iter := f.MapRange()
			for iter.Next() {
				lowerCaseKeys[iter.Key().Interface().(string)] = iter.Value().Interface()
			}
			s, err := json.MarshalToString(lowerCaseKeys)
			if err != nil {
				return nil, err
			}

			params.Set(paramName, s)
		}
	}
	return params, nil
}

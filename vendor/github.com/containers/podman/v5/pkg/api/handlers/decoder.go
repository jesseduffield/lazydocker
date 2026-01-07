//go:build !remote

package handlers

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/pkg/util"
	"github.com/gorilla/schema"
	"github.com/sirupsen/logrus"
	"go.podman.io/image/v5/types"
)

// NewAPIDecoder returns a configured schema.Decoder
func NewAPIDecoder() *schema.Decoder {
	d := schema.NewDecoder()
	d.IgnoreUnknownKeys(true)
	d.RegisterConverter(map[string][]string{}, convertURLValuesString)
	d.RegisterConverter(time.Time{}, convertTimeString)
	d.RegisterConverter(define.ContainerStatus(0), convertContainerStatusString)
	d.RegisterConverter(map[string]string{}, convertStringMap)

	var Signal syscall.Signal
	d.RegisterConverter(Signal, convertSignal)

	d.RegisterConverter(types.OptionalBoolUndefined, convertOptionalBool)

	return d
}

func NewCompatAPIDecoder() *schema.Decoder {
	dec := NewAPIDecoder()

	// mimic behaviour of github.com/docker/docker/api/server/httputils.BoolValue()
	dec.RegisterConverter(true, func(s string) reflect.Value {
		s = strings.ToLower(strings.TrimSpace(s))
		return reflect.ValueOf(s != "" && s != "0" && s != "no" && s != "false" && s != "none")
	})
	dec.RegisterConverter(types.OptionalBoolUndefined, func(s string) reflect.Value {
		if len(s) == 0 {
			return reflect.ValueOf(types.OptionalBoolUndefined)
		}
		s = strings.ToLower(strings.TrimSpace(s))
		if s != "0" && s != "no" && s != "false" && s != "none" {
			return reflect.ValueOf(types.OptionalBoolTrue)
		}
		return reflect.ValueOf(types.OptionalBoolFalse)
	})

	return dec
}

// On client:
//
//	v := map[string][]string{
//		"dangling": {"true"},
//	}
//
//	payload, err := jsoniter.MarshalToString(v)
//	if err != nil {
//		panic(err)
//	}
//	payload = url.QueryEscape(payload)
func convertURLValuesString(query string) reflect.Value {
	f := map[string][]string{}

	err := json.Unmarshal([]byte(query), &f)
	if err != nil {
		logrus.Infof("convertURLValuesString: Failed to Unmarshal %s: %s", query, err.Error())
	}

	return reflect.ValueOf(f)
}

func convertStringMap(query string) reflect.Value {
	res := make(map[string]string)
	err := json.Unmarshal([]byte(query), &res)
	if err != nil {
		logrus.Infof("convertStringMap: Failed to Unmarshal %s: %s", query, err.Error())
	}
	return reflect.ValueOf(res)
}

func convertContainerStatusString(query string) reflect.Value {
	result, err := define.StringToContainerStatus(query)
	if err != nil {
		logrus.Infof("convertContainerStatusString: Failed to parse %s: %s", query, err.Error())

		// We return nil here instead of result because reflect.ValueOf().IsValid() will be true
		// in github.com/gorilla/schema's decoder, which means there's no parsing error
		return reflect.ValueOf(nil)
	}

	return reflect.ValueOf(result)
}

// isZero() can be used to determine if parsing failed.
func convertTimeString(query string) reflect.Value {
	var (
		err error
		t   time.Time
	)

	for _, f := range []string{
		time.UnixDate,
		time.ANSIC,
		time.RFC1123,
		time.RFC1123Z,
		time.RFC3339,
		time.RFC3339Nano,
		time.RFC822,
		time.RFC822Z,
		time.RFC850,
		time.RubyDate,
		time.Stamp,
		time.StampMicro,
		time.StampMilli,
		time.StampNano,
	} {
		t, err = time.Parse(f, query)
		if err == nil {
			return reflect.ValueOf(t)
		}

		if _, isParseError := err.(*time.ParseError); isParseError {
			// Try next format
			continue
		} else {
			break
		}
	}

	// We've exhausted all formats, or something bad happened
	if err != nil {
		logrus.Infof("convertTimeString: Failed to parse %s: %s", query, err.Error())
	}
	return reflect.ValueOf(time.Time{})
}

func convertSignal(query string) reflect.Value {
	signal, err := util.ParseSignal(query)
	if err != nil {
		logrus.Infof("convertSignal: Failed to parse %s: %s", query, err.Error())
	}
	return reflect.ValueOf(signal)
}

func convertOptionalBool(s string) reflect.Value {
	if len(s) == 0 {
		return reflect.ValueOf(types.OptionalBoolUndefined)
	}
	val, _ := strconv.ParseBool(s)
	if val {
		return reflect.ValueOf(types.OptionalBoolTrue)
	}
	return reflect.ValueOf(types.OptionalBoolFalse)
}

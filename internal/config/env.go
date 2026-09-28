package config

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// EnvPrefix starts every environment variable that overrides a setting.
const EnvPrefix = "CHOWKI_"

// EnvVars returns the environment variable of every setting that one can
// override, such as CHOWKI_SERVER_LISTEN for server.listen, in file order.
// Lists, such as providers, can only be set in the file.
func EnvVars() []string {
	var names []string
	walkSettings(reflect.ValueOf(&Config{}).Elem(), EnvPrefix, func(name string, _ reflect.Value) {
		names = append(names, name)
	})
	return names
}

var durationType = reflect.TypeFor[time.Duration]()

// applyEnv sets every setting whose environment variable is set. Errors
// name the variable but never show its value, which may be a secret.
func applyEnv(cfg *Config, env func(string) (string, bool)) error {
	var errs []error
	walkSettings(reflect.ValueOf(cfg).Elem(), EnvPrefix, func(name string, field reflect.Value) {
		raw, ok := env(name)
		if !ok {
			return
		}
		if err := setField(field, raw); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		}
	})
	return errors.Join(errs...)
}

// walkSettings calls fn for every scalar setting in v, a struct, with the
// name of its environment variable.
func walkSettings(v reflect.Value, prefix string, fn func(name string, field reflect.Value)) {
	t := v.Type()
	for i := range t.NumField() {
		tag, _, _ := strings.Cut(t.Field(i).Tag.Get("yaml"), ",")
		if tag == "" || tag == "-" {
			continue
		}
		name := prefix + strings.ToUpper(tag)
		switch f := v.Field(i); f.Kind() {
		case reflect.Struct:
			walkSettings(f, name+"_", fn)
		case reflect.String, reflect.Int, reflect.Int64, reflect.Bool:
			fn(name, f)
		}
	}
}

func setField(field reflect.Value, raw string) error {
	switch {
	case field.Type() == durationType:
		d, err := time.ParseDuration(raw)
		if err != nil {
			return errors.New("not a duration, such as 600s or 10m")
		}
		field.SetInt(int64(d))
	case field.Kind() == reflect.String:
		field.SetString(raw)
	case field.Kind() == reflect.Int:
		n, err := strconv.Atoi(raw)
		if err != nil {
			return errors.New("not a whole number")
		}
		field.SetInt(int64(n))
	case field.Kind() == reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return errors.New("not true or false")
		}
		field.SetBool(b)
	}
	return nil
}

package copier

import (
	"database/sql"
	"errors"
	"reflect"
)

const (
	DisableScanner = iota
)

type CopyOption int

// Copy copy things
func Copy(toValue interface{}, fromValue interface{}, flags ...CopyOption) (err error) {
	var (
		isSlice bool
		amount  = 1
		from    = indirect(reflect.ValueOf(fromValue))
		to      = indirect(reflect.ValueOf(toValue))
		opts    = createOptions(flags)
	)

	if !to.CanAddr() {
		return errors.New("copy to value is unaddressable")
	}
	// Return is from value is invalid
	if !from.IsValid() {
		return
	}

	// Just set it if possible to assign
	if from.Type().AssignableTo(to.Type()) {
		to.Set(from)
		return
	}

	fromType := indirectType(from.Type())
	toType := indirectType(to.Type())

	if fromType.Kind() != reflect.Struct || toType.Kind() != reflect.Struct {
		return
	}

	if to.Kind() == reflect.Slice {
		isSlice = true
		if from.Kind() == reflect.Slice {
			amount = from.Len()
		}
	}

	for i := 0; i < amount; i++ {
		var dest, source reflect.Value

		if isSlice {
			// source
			if from.Kind() == reflect.Slice {
				source = indirect(from.Index(i))
			} else {
				source = indirect(from)
			}

			// dest
			dest = indirect(reflect.New(toType).Elem())
		} else {
			source = indirect(from)
			dest = indirect(to)
		}

		// Copy from field to field or method
		for _, field := range deepFields(fromType) {
			name := field.Name
			if fromField := source.FieldByName(name); fromField.IsValid() {
				// has field
				if toField := dest.FieldByName(name); toField.IsValid() {
					if toField.CanSet() {
						if !set(toField, fromField, opts) {
							if err := Copy(toField.Addr().Interface(), fromField.Interface()); err != nil {
								return err
							}
						}
					}
				} else {
					// try to set to method
					var toMethod reflect.Value
					if dest.CanAddr() {
						toMethod = dest.Addr().MethodByName(name)
					} else {
						toMethod = dest.MethodByName(name)
					}

					if toMethod.IsValid() && toMethod.Type().NumIn() == 1 && fromField.Type().AssignableTo(toMethod.Type().In(0)) {
						toMethod.Call([]reflect.Value{fromField})
					}
				}
			}
		}

		// Copy from method to field
		for _, field := range deepFields(toType) {
			name := field.Name
			var fromMethod reflect.Value
			if source.CanAddr() {
				fromMethod = source.Addr().MethodByName(name)
			} else {
				fromMethod = source.MethodByName(name)
			}

			if fromMethod.IsValid() && fromMethod.Type().NumIn() == 0 && fromMethod.Type().NumOut() == 1 {
				if toField := dest.FieldByName(name); toField.IsValid() && toField.CanSet() {
					values := fromMethod.Call([]reflect.Value{})
					if len(values) >= 1 {
						set(toField, values[0])
					}
				}
			}
		}

		if isSlice {
			if dest.Addr().Type().AssignableTo(to.Type().Elem()) {
				to.Set(reflect.Append(to, dest.Addr()))
			} else if dest.Type().AssignableTo(to.Type().Elem()) {
				to.Set(reflect.Append(to, dest))
			}
		}
	}
	return
}

func deepFields(reflectType reflect.Type) []reflect.StructField {
	var fields []reflect.StructField

	if reflectType = indirectType(reflectType); reflectType.Kind() == reflect.Struct {
		for i := 0; i < reflectType.NumField(); i++ {
			v := reflectType.Field(i)
			if v.Anonymous {
				fields = append(fields, deepFields(v.Type)...)
			} else {
				fields = append(fields, v)
			}
		}
	}

	return fields
}

func indirect(reflectValue reflect.Value) reflect.Value {
	for reflectValue.Kind() == reflect.Ptr {
		reflectValue = reflectValue.Elem()
	}
	return reflectValue
}

func indirectType(reflectType reflect.Type) reflect.Type {
	for reflectType.Kind() == reflect.Ptr || reflectType.Kind() == reflect.Slice {
		reflectType = reflectType.Elem()
	}
	return reflectType
}

func set(to, from reflect.Value, options ...map[CopyOption]bool) bool {
	opts := make(map[CopyOption]bool)
	if len(options) != 0 {
		opts = options[0]
	}
	if from.IsValid() {
		if to.Kind() == reflect.Ptr {
			// This won't stomp down on TO with a nil value. So if TO is already populated, not sure
			// want expected behavior is (copy vs merge)
			if (from.Kind() == reflect.Ptr || from.Kind() == reflect.Map || from.Kind() == reflect.Slice) && from.IsNil() {
				return true // leave to as Nil
			} else if from.Type().Comparable() && from.Interface() == reflect.Zero(from.Type()).Interface() {
				return true // if from is zero valued, leave to pointer as nil
			}
			if to.IsNil() {
				to.Set(reflect.New(to.Type().Elem()))
			}
			to = to.Elem()
		}
		if from.Type().ConvertibleTo(to.Type()) {
			to.Set(from.Convert(to.Type()))
		} else if scanner, ok := to.Addr().Interface().(sql.Scanner); ok && !opts[DisableScanner] {
			scanner.Scan(from.Interface())
		} else if from.Kind() == reflect.Ptr {
			return set(to, from.Elem())
		} else {
			return false
		}
	}
	return true
}

func createOptions(flags []CopyOption) map[CopyOption]bool {
	opt := make(map[CopyOption]bool)
	for _, o := range flags {
		opt[o] = true
	}
	return opt
}

package facts

import "reflect"

// Clone returns a copy of the analysis that shares no slice, map or pointer with it, so that
// changing one never changes the other.
func (a *Analysis) Clone() *Analysis {
	if a == nil {
		return nil
	}
	return deepCopy(reflect.ValueOf(a)).Interface().(*Analysis)
}

func deepCopy(v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return v
		}
		out := reflect.New(v.Elem().Type())
		out.Elem().Set(deepCopy(v.Elem()))
		return out
	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := range v.Len() {
			out.Index(i).Set(deepCopy(v.Index(i)))
		}
		return out
	case reflect.Map:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeMapWithSize(v.Type(), v.Len())
		for _, k := range v.MapKeys() {
			out.SetMapIndex(k, deepCopy(v.MapIndex(k)))
		}
		return out
	case reflect.Struct:
		// Every field of a fact is exported, so every one can be set.
		out := reflect.New(v.Type()).Elem()
		for i := range v.NumField() {
			out.Field(i).Set(deepCopy(v.Field(i)))
		}
		return out
	case reflect.Array:
		out := reflect.New(v.Type()).Elem()
		for i := range v.Len() {
			out.Index(i).Set(deepCopy(v.Index(i)))
		}
		return out
	}
	return v
}

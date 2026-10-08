package workflow

import (
	"fmt"
	"reflect"
	"slices"
)

func logDryRunWorkflowChanges(before, after *WorkflowData) {
	if compilerDevelopmentLog.Enabled() {
		logDryRunValueChanges("", reflect.ValueOf(before), reflect.ValueOf(after))
	}
}

func logDryRunValueChanges(path string, before, after reflect.Value) {
	if !before.IsValid() || !after.IsValid() || before.Type() != after.Type() {
		compilerDevelopmentLog.Printf("Dry-run mutation: %q", path)
		return
	}
	if before.Comparable() && after.Comparable() && before.Equal(after) {
		return
	}
	switch before.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !before.IsNil() && !after.IsNil() {
			logDryRunValueChanges(path, before.Elem(), after.Elem())
			return
		}
	case reflect.Struct:
		for i := range before.NumField() {
			field := before.Type().Field(i)
			if field.PkgPath == "" {
				name := field.Name
				if path != "" {
					name = path + "." + name
				}
				logDryRunValueChanges(name, before.Field(i), after.Field(i))
			}
		}
		return
	case reflect.Map:
		if before.Type().Key().Kind() == reflect.String {
			logDryRunMapChanges(path, before, after)
			return
		}
	case reflect.Slice:
		if before.Len() == after.Len() {
			for i := range before.Len() {
				logDryRunValueChanges(fmt.Sprintf("%s[%d]", path, i), before.Index(i), after.Index(i))
			}
			return
		}
	}
	compilerDevelopmentLog.Printf("Dry-run mutation: %q", path)
}

func logDryRunMapChanges(path string, before, after reflect.Value) {
	keys := make(map[string]reflect.Value)
	for _, value := range []reflect.Value{before, after} {
		for _, key := range value.MapKeys() {
			keys[key.String()] = key
		}
	}
	names := make([]string, 0, len(keys))
	for name := range keys {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		key := keys[name]
		logDryRunValueChanges(path+"."+name, before.MapIndex(key), after.MapIndex(key))
	}
}

package host

import (
	"encoding/json"
	"maps"
	"path"
	"reflect"
	"slices"
	"strings"
	"time"
)

type schemaDefs map[string]any

func Schema() ([]byte, error) {
	defs := schemaDefs{}
	numbered := map[string]any{"id": map[string]any{"type": []string{"string", "integer"}}}
	var server, client, results []any
	notified, requested, answered := map[string]any{}, map[string]any{}, map[reflect.Type]bool{}
	for _, one := range notifications() {
		params := defs.of(reflect.TypeOf(one.params))
		notified[one.name] = params
		server = append(server, envelope(one.name, params, nil))
	}
	for _, one := range requests() {
		params, result := defs.of(reflect.TypeOf(one.params)), defs.of(reflect.TypeOf(one.result))
		requested[one.name] = map[string]any{"params": params, "result": result}
		client = append(client, envelope(one.name, params, numbered))
		if shape := reflect.TypeOf(one.result); !answered[shape] {
			answered[shape], results = true, append(results, result)
		}
	}
	asked, answer := defs.of(reflect.TypeOf(ApprovalRequest{})), defs.of(reflect.TypeOf(ApprovalAnswer{}))
	server = append(server, envelope(ApprovalMethod, asked, numbered), reply("result", map[string]any{"anyOf": results}), reply("error", defs.of(reflect.TypeOf(Refusal{}))))
	defs["clientMessage"] = map[string]any{"oneOf": append(client, reply("result", answer))}
	return json.MarshalIndent(map[string]any{
		"$schema":         "https://json-schema.org/draft/2020-12/schema",
		"$id":             Protocol,
		"title":           Protocol + ", every line tofu serve --stdio writes; clientMessage is every line it reads",
		"oneOf":           server,
		"$defs":           defs,
		"x-notifications": notified,
		"x-requests":      requested,
		"x-serverRequests": map[string]any{
			ApprovalMethod: map[string]any{"params": asked, "result": answer},
		},
	}, "", "  ")
}

func envelope(method string, params any, more map[string]any) map[string]any {
	properties := map[string]any{"jsonrpc": map[string]any{"const": rpcVersion}, "method": map[string]any{"const": method}, "params": params}
	maps.Copy(properties, more)
	return closed(properties, slices.Sorted(maps.Keys(properties)))
}

func reply(field string, body any) map[string]any {
	properties := map[string]any{"jsonrpc": map[string]any{"const": rpcVersion}, "id": map[string]any{"type": []string{"string", "integer", "null"}}, field: body}
	return closed(properties, slices.Sorted(maps.Keys(properties)))
}

func closed(properties map[string]any, required []string) map[string]any {
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}

func (defs schemaDefs) of(t reflect.Type) any {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if named, isEnum := reflect.Zero(t).Interface().(enumerated); isEnum {
		return map[string]any{"type": "string", "enum": named.enum()}
	}
	switch t {
	case reflect.TypeFor[time.Time]():
		return map[string]any{"type": "string", "format": "date-time"}
	case reflect.TypeFor[json.RawMessage]():
		return map[string]any{}
	}
	switch t.Kind() {
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int64:
		return map[string]any{"type": "integer"}
	case reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Interface:
		return map[string]any{}
	case reflect.Slice:
		return map[string]any{"type": []string{"array", "null"}, "items": defs.of(t.Elem())}
	case reflect.Map:
		return map[string]any{"type": []string{"object", "null"}, "additionalProperties": defs.of(t.Elem())}
	case reflect.Struct:
		name := t.Name()
		if t.PkgPath() != reflect.TypeFor[Identity]().PkgPath() {
			name = path.Base(t.PkgPath()) + "." + name
		}
		if _, done := defs[name]; !done {
			defs[name] = map[string]any{}
			properties, required := map[string]any{}, []string{}
			defs.fields(t, properties, &required)
			defs[name] = closed(properties, required)
		}
		return map[string]any{"$ref": "#/$defs/" + name}
	}
	panic("host: no schema for " + t.String())
}

func (defs schemaDefs) fields(t reflect.Type, properties map[string]any, required *[]string) {
	for index := range t.NumField() {
		field := t.Field(index)
		if field.Anonymous {
			defs.fields(field.Type, properties, required)
			continue
		}
		name, options, _ := strings.Cut(field.Tag.Get("json"), ",")
		if !field.IsExported() || name == "-" {
			continue
		}
		properties[name] = defs.of(field.Type)
		if options != "omitempty" && options != "omitzero" {
			*required = append(*required, name)
		}
	}
}

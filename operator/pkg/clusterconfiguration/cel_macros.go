package clusterconfiguration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"sigs.k8s.io/yaml"

	pkgsecrets "github.com/redpanda-data/redpanda-operator/operator/pkg/secrets"
)

// These are provided in case any external code wants to construct CEL expressions.
const (
	CELEnvString             = "envString"
	CELRepr                  = "repr"
	CELAppendYamlStringArray = "appendYamlStringArray"
	CELExternalSecretRef     = "externalSecretRef"
	CELErrorToWarning        = "errorToWarning"
)

func StdLibFactory(ctx context.Context, environ map[string]string, cloudExpander *pkgsecrets.CloudExpander) CelFactory {
	return func(v reflect.Value) (*cel.Env, error) {
		cel.StdLib()
		return cel.NewEnv(
			cel.Variable("it", cel.AnyType),
			wrapStringToString(CELEnvString, envString(environ)),
			wrapStringToString(CELRepr, repr),
			wrapStringStringToString(CELAppendYamlStringArray, appendYamlStringArray),
			wrapStringToString(CELExternalSecretRef, externalSecretRef(ctx, cloudExpander)),
			wrapStringToString("makeError", makeError),
			errorToWarning(),
		)
	}
}

func errorToWarning() cel.EnvOption {
	return cel.Function(CELErrorToWarning,
		cel.Overload(fmt.Sprintf("%s_any", CELErrorToWarning), []*cel.Type{cel.AnyType}, cel.AnyType,
			cel.UnaryBinding(func(arg1 ref.Val) ref.Val {
				if err, isErr := arg1.(*types.Err); isErr {
					return types.WrapErr(Warning{err})
				}
				return arg1
			}),
			cel.OverloadIsNonStrict(),
		),
	)
}

func wrapStringToString(name string, f func(string) (string, error)) cel.EnvOption {
	return cel.Function(name,
		cel.Overload(fmt.Sprintf("%s_string", name), []*cel.Type{cel.StringType}, cel.StringType,
			cel.UnaryBinding(func(arg1 ref.Val) ref.Val {
				req, err := arg1.ConvertToNative(reflect.TypeFor[string]())
				if err != nil {
					return types.WrapErr(err)
				}
				res, err := f(req.(string))
				if err != nil {
					return types.WrapErr(err)
				}
				return types.String(res)
			}),
		),
	)
}

func wrapStringStringToString(name string, f func(string, string) (string, error)) cel.EnvOption {
	return cel.Function(name,
		cel.Overload(fmt.Sprintf("%s_string_string", name), []*cel.Type{cel.StringType, cel.StringType}, cel.StringType,
			cel.BinaryBinding(func(arg1 ref.Val, arg2 ref.Val) ref.Val {
				req1, err := arg1.ConvertToNative(reflect.TypeFor[string]())
				if err != nil {
					return types.WrapErr(err)
				}
				req2, err := arg2.ConvertToNative(reflect.TypeFor[string]())
				if err != nil {
					return types.WrapErr(err)
				}
				res, err := f(req1.(string), req2.(string))
				if err != nil {
					return types.WrapErr(err)
				}
				return types.String(res)
			}),
		),
	)
}

// envString looks up an environment variable in the provided environment
// and returns that value. It's an error if the value is not provided.
func envString(environ map[string]string) func(string) (string, error) {
	return func(envName string) (string, error) {
		v, ok := environ[envName]
		if !ok {
			return "", fmt.Errorf("not found in environment: %q", envName)
		}
		return v, nil
	}
}

// repr is a utility function for bootstrap.yaml fixups; because we need to
// return a YAML-compatible serialisation, we might need to wrap this around
// CEL-computed values.
func repr(value string) (string, error) {
	buf, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(buf), nil
}

// appendYamlStringArray is a highly-specific function for use in bootstrap.yaml
// fixups. It takes the *representation* of a string array and a string, and adds
// the string to the serialised array, returning a JSON/YAML-compatible serialisation
// of the new array.
func appendYamlStringArray(arrayRepr string, addend string) (string, error) {
	var array []string
	// We YAML-unmarshal, as always, to be as flexible as possible
	if err := yaml.Unmarshal([]byte(arrayRepr), &array); err != nil {
		return "", fmt.Errorf("cannot unmarshal YAML string array: %w", err)
	}
	array = append(array, addend)
	// We use a stricter, one-line format to remarshal the result
	buf, err := json.Marshal(array)
	if err != nil {
		return "", err
	}
	return string(buf), err
}

// externalSecretRef pulls back the contents of an external secret as a string
func externalSecretRef(ctx context.Context, cloudExpander *pkgsecrets.CloudExpander) func(string) (string, error) {
	return func(secretRef string) (string, error) {
		if cloudExpander == nil {
			return "", fmt.Errorf("cannot derefence %q: external secret references are unsupported", secretRef)
		}
		expanded, err := cloudExpander.Expand(ctx, secretRef)
		if err != nil {
			return "", fmt.Errorf("configuration entry %q: trouble expanding external secret reference: %w", secretRef, err)
		}
		return expanded, nil
	}
}

// makeError is an internal macro that's used for testing to generate errors
func makeError(errMsg string) (string, error) {
	return "", errors.New(errMsg)
}

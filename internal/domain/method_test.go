package domain

import "testing"

func TestTypeRef_String(t *testing.T) {
	tests := []struct {
		name string
		tr   TypeRef
		want string
	}{
		{
			name: "simple type",
			tr:   TypeRef{Name: "string"},
			want: "string",
		},
		{
			name: "pointer type",
			tr:   TypeRef{Name: "Model", IsPointer: true},
			want: "*Model",
		},
		{
			name: "slice type",
			tr:   TypeRef{Name: "string", IsSlice: true},
			want: "[]string",
		},
		{
			name: "qualified type",
			tr:   TypeRef{Name: "Context", Package: "context"},
			want: "context.Context",
		},
		{
			name: "pointer to qualified type",
			tr:   TypeRef{Name: "Service", Package: "internal/service", IsPointer: true},
			want: "*internal/service.Service",
		},
		{
			name: "slice of pointer to qualified type",
			tr:   TypeRef{Name: "Model", Package: "domain", IsPointer: true, IsSlice: true},
			want: "[]*domain.Model",
		},
		{
			name: "simple map",
			tr: TypeRef{
				IsMap: true,
				KeyType: &TypeRef{
					Name: "string",
				},
				ValueType: &TypeRef{
					Name: "int",
				},
			},
			want: "map[string]int",
		},
		{
			name: "map with qualified value",
			tr: TypeRef{
				IsMap: true,
				KeyType: &TypeRef{
					Name: "string",
				},
				ValueType: &TypeRef{
					Name:    "Model",
					Package: "domain",
				},
			},
			want: "map[string]domain.Model",
		},
		{
			name: "map with pointer value",
			tr: TypeRef{
				IsMap: true,
				KeyType: &TypeRef{
					Name: "string",
				},
				ValueType: &TypeRef{
					Name:      "Model",
					IsPointer: true,
				},
			},
			want: "map[string]*Model",
		},
		{
			name: "generic named type",
			tr: TypeRef{
				Name:     "Box",
				Package:  "domain",
				TypeArgs: []TypeRef{{Name: "T"}},
			},
			want: "domain.Box[T]",
		},
		{
			name: "slice of generic named type",
			tr: TypeRef{
				Name:     "Box",
				IsSlice:  true,
				TypeArgs: []TypeRef{{Name: "T"}},
			},
			want: "[]Box[T]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.tr.String()
			if got != tt.want {
				t.Errorf("TypeRef.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParamDef_String(t *testing.T) {
	tests := []struct {
		name string
		pd   ParamDef
		want string
	}{
		{
			name: "named parameter",
			pd: ParamDef{
				Name: "ctx",
				Type: TypeRef{Name: "Context", Package: "context"},
			},
			want: "ctx context.Context",
		},
		{
			name: "unnamed parameter",
			pd: ParamDef{
				Name: "",
				Type: TypeRef{Name: "error"},
			},
			want: "error",
		},
		{
			name: "pointer parameter",
			pd: ParamDef{
				Name: "model",
				Type: TypeRef{Name: "Model", IsPointer: true},
			},
			want: "model *Model",
		},
		{
			name: "slice parameter",
			pd: ParamDef{
				Name: "items",
				Type: TypeRef{Name: "string", IsSlice: true},
			},
			want: "items []string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.pd.String()
			if got != tt.want {
				t.Errorf("ParamDef.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMethodDef_Signature(t *testing.T) {
	tests := []struct {
		name string
		md   MethodDef
		want string
	}{
		{
			name: "no params no returns",
			md: MethodDef{
				Name: "DoSomething",
			},
			want: "DoSomething()",
		},
		{
			name: "single param no returns",
			md: MethodDef{
				Name: "Process",
				Params: []ParamDef{
					{Name: "data", Type: TypeRef{Name: "string"}},
				},
			},
			want: "Process(data string)",
		},
		{
			name: "multiple params no returns",
			md: MethodDef{
				Name: "Configure",
				Params: []ParamDef{
					{Name: "ctx", Type: TypeRef{Name: "Context", Package: "context"}},
					{Name: "opts", Type: TypeRef{Name: "Options", IsPointer: true}},
				},
			},
			want: "Configure(ctx context.Context, opts *Options)",
		},
		{
			name: "no params single return",
			md: MethodDef{
				Name: "GetName",
				Returns: []TypeRef{
					{Name: "string"},
				},
			},
			want: "GetName() string",
		},
		{
			name: "no params multiple returns",
			md: MethodDef{
				Name: "GetValue",
				Returns: []TypeRef{
					{Name: "string"},
					{Name: "error"},
				},
			},
			want: "GetValue() (string, error)",
		},
		{
			name: "full method signature",
			md: MethodDef{
				Name: "Generate",
				Params: []ParamDef{
					{Name: "ctx", Type: TypeRef{Name: "Context", Package: "context"}},
					{Name: "paths", Type: TypeRef{Name: "string", IsSlice: true}},
				},
				Returns: []TypeRef{
					{Name: "Result", IsSlice: true},
					{Name: "error"},
				},
			},
			want: "Generate(ctx context.Context, paths []string) ([]Result, error)",
		},
		{
			name: "method with pointer return",
			md: MethodDef{
				Name: "NewService",
				Returns: []TypeRef{
					{Name: "Service", IsPointer: true},
				},
			},
			want: "NewService() *Service",
		},
		{
			name: "generic function-style signature",
			md: MethodDef{
				Name: "Map",
				TypeParams: []ParamDef{
					{Name: "T", Type: TypeRef{Name: "any"}},
					{Name: "R", Type: TypeRef{Name: "comparable"}},
				},
				Params: []ParamDef{
					{Name: "items", Type: TypeRef{Name: "T", IsSlice: true}},
					{Name: "fn", Type: TypeRef{Name: "func(T) R"}},
				},
				Returns: []TypeRef{{Name: "R", IsSlice: true}},
			},
			want: "Map[T any, R comparable](items []T, fn func(T) R) []R",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.md.Signature()
			if got != tt.want {
				t.Errorf("MethodDef.Signature() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMethodDef_SignatureWithVisibility(t *testing.T) {
	tests := []struct {
		name string
		md   MethodDef
		want string
	}{
		{
			name: "exported method",
			md: MethodDef{
				Name:       "DoSomething",
				IsExported: true,
			},
			want: "+DoSomething()",
		},
		{
			name: "unexported method",
			md: MethodDef{
				Name:       "doSomething",
				IsExported: false,
			},
			want: "-doSomething()",
		},
		{
			name: "exported method with params and returns",
			md: MethodDef{
				Name:       "Generate",
				IsExported: true,
				Params: []ParamDef{
					{Name: "ctx", Type: TypeRef{Name: "Context", Package: "context"}},
				},
				Returns: []TypeRef{
					{Name: "error"},
				},
			},
			want: "+Generate(ctx context.Context) error",
		},
		{
			name: "unexported method with params and returns",
			md: MethodDef{
				Name:       "validate",
				IsExported: false,
				Params: []ParamDef{
					{Name: "data", Type: TypeRef{Name: "string"}},
				},
				Returns: []TypeRef{
					{Name: "error"},
				},
			},
			want: "-validate(data string) error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.md.SignatureWithVisibility()
			if got != tt.want {
				t.Errorf("MethodDef.SignatureWithVisibility() = %q, want %q", got, tt.want)
			}
		})
	}
}

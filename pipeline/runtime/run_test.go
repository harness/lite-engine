package runtime

import (
	"reflect"
	"testing"
)

func Test_splitCommands(t *testing.T) {
	tests := []struct {
		command string
		want    []string
	}{
		{
			command: `echo "hello"; echo "world"`,
			want:    []string{`echo "hello"`, `echo "world"`},
		},
		{
			command: `echo "hello\nworld"; echo "foo\nbar"`,
			want:    []string{`echo "hello\nworld"`, `echo "foo\nbar"`},
		},
		{
			command: `echo 'hello;world'; echo 'foo;bar'`,
			want:    []string{`echo 'hello;world'`, `echo 'foo;bar'`},
		},
		{
			command: `echo "hello"; echo 'world'`,
			want:    []string{`echo "hello"`, `echo 'world'`},
		},
		{
			command: `echo "hello world"; echo 'foo bar'`,
			want:    []string{`echo "hello world"`, `echo 'foo bar'`},
		},
		{
			command: `;echo "hello";; echo "world";`,
			want:    []string{`echo "hello"`, `echo "world"`},
		},
		{
			command: `   echo "hello"   ;    echo "world"   `,
			want:    []string{`echo "hello"`, `echo "world"`},
		},
	}

	for _, test := range tests {
		result := splitCommands(test.command)
		if !reflect.DeepEqual(result, test.want) {
			t.Errorf("splitCommands(%q) = %v; want %v", test.command, result, test.want)
		}
	}
}

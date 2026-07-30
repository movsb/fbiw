package style

import (
	"gofb/utils"
	"reflect"
	"testing"
)

type StyleParseTest struct {
	Style string
	Rules []Rule
}

func TestCalc(t *testing.T) {
	cases := utils.LoadTestCases[StyleParseTest](`testdata/style.yaml`)
	for i, tc := range cases {
		sheet, err := ParseStyle([]byte(tc.Style))
		if err != nil {
			t.Error(err)
			continue
		}
		if i == 6 {
			i += 0
		}
		for i, r := range tc.Rules {
			if len(r.Declarations) == 0 {
				tc.Rules[i].Declarations = nil
			}
		}
		if !reflect.DeepEqual(tc.Rules, sheet.Rules) {
			t.Errorf("解析不一致：\nwant: %v\ngot:  %v", tc.Rules, sheet.Rules)
			continue
		}
	}
}

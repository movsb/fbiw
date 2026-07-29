package main

import "testing"

type BoxTest struct {
	HTML string            `yaml:"html"`
	Calc map[string][4]int `yaml:"calc"`
}

func getByID(root Box, id string) Box {
	if root.Base().ID == id {
		return root
	}
	for _, child := range root.Base().Children {
		if box := getByID(child, id); box != nil {
			return box
		}
	}
	panic(`指定ID的盒子没找到`)
}

func TestCalc(t *testing.T) {
	cases := loadTestCases[BoxTest](`testdata/box.yaml`)
	for i, tc := range cases {
		root := loadBox([]byte(tc.HTML))
		if i == 2 {
			i += 0
		}
		root.Calc(1024, 768)
		for id, rect := range tc.Calc {
			box := getByID(root, id)
			pos := box.Base().calcPos
			if pos.X != rect[0] ||
				pos.Y != rect[1] ||
				pos.Width != rect[2] ||
				pos.Height != rect[3] {
				t.Errorf(`排版错误：#%d, id: %s, want: %v -> got: %v`, i, id, rect, pos)
			}
		}
	}
}

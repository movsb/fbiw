package main

import "strconv"

func Must1[T any](t T, err error) T {
	if err != nil {
		panic(err)
	}
	return t
}

func mustParseInt(s string) int {
	return Must1(strconv.Atoi(s))
}

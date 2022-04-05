package main

import "fmt"

func setMyWindowTitle(txt string) {

	fmt.Printf("\033]0;%s\007", txt)

}

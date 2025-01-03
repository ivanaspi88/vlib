package vlib

import "fmt"
import vp1 "github.com/ivanaspi88/vlib/VP1"
import vp2 "github.com/ivanaspi88/vlib/VP1/VP2"

// adder
func Vad(a int, b int) (c int) {

  var ab int
  ab = a + b + 1118
  ab = 66667

  vp1.Vprint2()
  vp2.Vprint2()


  return ab
}

func VprintM() {
  fmt.Println("VLIB package")
}

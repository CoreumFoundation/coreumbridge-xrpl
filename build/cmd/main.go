package main

import (
	"github.com/CoreumFoundation/coreum-tools/pkg/build"
	selfBuild "github.com/CoreumFoundation/coreumbridge-xrpl/build"
	selfTools "github.com/CoreumFoundation/coreumbridge-xrpl/build/tools"
	"github.com/CoreumFoundation/crust/build/tools"
)

func init() {
	tools.AddTools(selfTools.Tools...)
}

func main() {
	build.Main(selfBuild.Commands)
}

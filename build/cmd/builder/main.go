package main

import (
	coreumTools "github.com/CoreumFoundation/coreum/build/tools"
	selfBuild "github.com/CoreumFoundation/coreumbridge-xrpl/build"
	selfTools "github.com/CoreumFoundation/coreumbridge-xrpl/build/tools"
	"github.com/CoreumFoundation/crust/build"
	"github.com/CoreumFoundation/crust/build/tools"
)

func init() {
	tools.AddTools(coreumTools.Tools...)
	tools.AddTools(selfTools.Tools...)
}

func main() {
	build.Main(selfBuild.Commands)
}

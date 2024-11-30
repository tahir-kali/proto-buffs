#!/bin/bash
protoc --go_out=$PWD --go_opt=module=$1 $2.proto
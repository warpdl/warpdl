package main

func init() {
	rsFlags = append(rsFlags, infoFlags...)
	dlFlags = append(dlFlags, rsFlags...)
}

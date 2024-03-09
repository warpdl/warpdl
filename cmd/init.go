package cmd

func init() {
	rsFlags = append(rsFlags, infoFlags...)
	dlFlags = append(dlFlags, rsFlags...)
}

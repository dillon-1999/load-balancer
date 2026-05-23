package main

import (
	"flag"
	"fmt"
	"net/http"
)

func helloHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Hello")
}
func main() {
	portPtr := flag.String("port", "", "Port to run server on")
	flag.Parse()

	http.HandleFunc("/", helloHandler)
	if err := http.ListenAndServe(fmt.Sprintf(":%s", *portPtr), nil); err != nil {
		panic(err)
	}
}

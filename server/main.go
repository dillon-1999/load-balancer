package main

import (
	"flag"
	"fmt"
	"net/http"
)

func helloHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Received request")
	fmt.Fprintf(w, "Hello")
}
func main() {
	portPtr := flag.String("port", "8080", "Port to run server on")
	flag.Parse()

	http.HandleFunc("/", helloHandler)
	if err := http.ListenAndServe(fmt.Sprintf(":%s", *portPtr), nil); err != nil {
		panic(err)
	}
}

all:
	mkdir h2a_darwin_amd64
	GOARCH=amd64 GOOS=darwin go build h2a.go
	mv h2a h2a_darwin_amd64/h2a
	zip h2a_darwin_amd64.zip -r h2a_darwin_amd64
	rm -rf h2a_darwin_amd64
	
	mkdir h2a_linux_amd64
	GOARCH=amd64 GOOS=linux go build h2a.go
	mv h2a h2a_linux_amd64/h2a
	zip h2a_linux_amd64.zip -r h2a_linux_amd64
	rm -rf h2a_linux_amd64

clean:
	rm ./*.zip

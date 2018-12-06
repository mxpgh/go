# Binary name
BINARY=imageCreator
# Builds the project
build:
		go build -o ${BINARY}
		go test -v
# Installs our project: copies binaries
install:
		go install
release:
		# Clean
		go clean
		rm -rf *.gz
		# Build for mac
		go build
		tar czvf imageCreator-mac64-${VERSION}.tar.gz ./imageCreator
		# Build for linux
		go clean
		CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build
		tar czvf imageCreator-linux64-${VERSION}.tar.gz ./imageCreator
		# Build for linux arm
		go clean
		CGO_ENABLED=0 GOOS=linux GOARCH=arm go build
		tar czvf imageCreator-linux-arm-${VERSION}.tar.gz ./imageCreator
		# Build for win
		go clean
		CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build
		tar czvf imageCreator-win64-${VERSION}.tar.gz ./imageCreator.exe
		go clean
# Cleans our projects: deletes binaries
clean:
		go clean

.PHONY:  clean build

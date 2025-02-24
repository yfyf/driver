# Dividat Driver

[![Build status](https://badge.buildkite.com/6a69682e2acf50cec89f8c64935b8b591beda5635db479b92a.svg)](https://buildkite.com/dividat/driver)

Dividat drivers and hardware test suites.

## Development

### Prerequisites

[Nix](https://nixos.org/nix) is required for installing dependencies and providing a suitable development environment.

### Quick start

- Enter the nix development shell: `nix develop`
- Build the driver: `make`
- Run the driver: `./bin/dividat-driver`

### Tests

Run the test suite with: `make test`.

### Go modules

To install a module, use `go get github.com/owner/repo`.

Documentation is available at https://golang.org/ref/mod.

### Formatting

Normalize formatting with: `make format`.

Code is formatted with `gofmt` and normalized formatting is required for CI to pass.

### Releasing

#### Building

**Currently releases can only be made from Linux.**

To create a release run: `make release`.

A default nix shell (defined in `nix/devShell.nix`) provides all necessary dependencies for building on your native system (i.e. Linux or Darwin). Running `make` will create a binary that should run on your system (at least in the default environemnt).

Releases are built as statically linked binaries for windows and linux using the cross compilation toolchain provided by nix. The toolchain is provided by nix shells defined in [crossBuild.nix](nix/crossBuild.nix). Building the binaries can be done by running `make crossbuild` from the default shell.

Existing release targets:

- Linux: x86_64 (statically linked with [musl](https://www.musl-libc.org/))
- Windows: x86_64

There are also build shells for macOS binaries, but these are not hooked into `make crossbuild` as currently they only work on macOS.
To build the macOS binaries:

```sh
export VERSION=$(git describe --always HEAD)

# Build for aarch64 / arm64 / Apple silicon
nix develop .\#crossBuild.darwin.aarch64 --command ./build.sh -v "$VERSION" -i src/dividat-driver/main.go -o ./bin/dividat-driver-darwin-arm64

# Build for x86_64 / amd64 / Intel
nix develop .\#crossBuild.darwin.x86_64 --command ./build.sh -v "$VERSION" -i src/dividat-driver/main.go -o ./bin/dividat-driver-darwin-amd64
```

### Deploying

To deploy a new release run: `make deploy`. This can only be done if you have correctly tagged the revision and have AWS credentials set in your environment.

## Installation

### Windows

This application can be run as a Windows service (<https://docs.microsoft.com/en-us/powershell/module/microsoft.powershell.management/new-service>).

A PowerShell script is provided to download and install the latest version as a Windows service. Run it with the following command in a PowerShell.

**Note:** You need to run it as an administrator.

```
PS C:\ Set-ExecutionPolicy Bypass -Scope Process -Force; iex ((New-Object System.Net.WebClient).DownloadString('https://raw.githubusercontent.com/dividat/driver/main/install.ps1'))
```

Please have a look at the [script](install.ps1) before running it on your system.

## Compatibility

To be able to connect to the driver from within a web app delivered over HTTPS, browsers need to consider the loopback address as a trustworthy origin even when not using TLS. This is the case for most modern browsers, with the exception of Safari (https://bugs.webkit.org/show_bug.cgi?id=171934).

This application supports the [Private Network Access](https://wicg.github.io/private-network-access/) headers to help browsers decide which web apps may connect to it. The default list of [permissible origins](https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Origin#syntax) consists of Dividat's app hosts. To restrict to a single origin or whitelist other origins, add one or more `--permissible-origin` parameters to the driver application.

## Tools

### Data recorder

#### Senso data

Data from Senso can be recorded using the [`recorder`](src/dividat-driver/recorder). Start it with `make record > foo.dat`. The created recording can be used by the replayer.

#### Senso Flex data

Like Senso data, but with `make record-flex`.

### Data replayer

Recorded data can be replayed for debugging purposes.

For default settings: `npm run replay`

To replay an other recording: `npm run replay -- rec/senso/simple.dat`

To change the replay speed: `npm run replay -- --speed=0.5 rec/senso/simple.dat`

To run without looping: `npm run replay -- --once`

#### Senso replay

The Senso replayer will appear as a Senso network device, so both driver and replayer should be running at the same time.

#### Senso Flex replay

The Senso Flex replayer (`npm run replay-flex`) supports the same parameters as the Senso replayer. It mocks the driver with respect to the `/flex` WebSocket resource, so the driver can not be running at the same time.

#### Senso Flex data converter

[tools/parse/flex-to-json.py](tools/parse/flex-to-json.py) can be used to convert Flex recordings from binary
to JSON. E.g.

    ./tools/parse/flex-to-json.py rec/flex/steps.dat

See [tools/parse/flex-to-json.py](tools/parse/flex-to-json.py) for more details.

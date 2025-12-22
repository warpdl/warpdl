<br/>
<p align="center">
  <a href="https://github.com/warpdl/warpdl">
    <img src="https://avatars.githubusercontent.com/u/134059456" alt="Logo" width="80" height="80">
  </a>

  <h3 align="center">WarpDL</h3>

  <p align="center">
    A powerful and versatile cross-platform download manager.
    <br/>
    <br/>
    <a href="https://github.com/warpdl/warpdl/issues">Report Bug</a>
    .
    <a href="https://github.com/warpdl/warpdl/issues">Request Feature</a>
  </p>
</p>

[![CI](https://github.com/warpdl/warpdl/actions/workflows/ci.yml/badge.svg)](https://github.com/warpdl/warpdl/actions/workflows/ci.yml) [![Release](https://github.com/warpdl/warpdl/actions/workflows/release.yml/badge.svg)](https://github.com/warpdl/warpdl/actions/workflows/release.yml) ![Downloads](https://img.shields.io/github/downloads/warpdl/warpdl/total) ![Contributors](https://img.shields.io/github/contributors/warpdl/warpdl?color=dark-green) ![Issues](https://img.shields.io/github/issues/warpdl/warpdl) ![License](https://img.shields.io/github/license/warpdl/warpdl) 


**Note**: This branch is the development branch. For the latest stable release, please refer to the [main branch](https://github.com/warpdl/warpdl/tree/main).

## Table Of Contents

* [About the Project](#about-the-project)
* [Getting Started](#getting-started)
  * [Prerequisites](#prerequisites)
  * [Installation](#installation)
* [Usage](#usage)
* [Roadmap](#roadmap)
* [Contributing](#contributing)
* [License](#license)

## About The Project

![Screen Shot](./screenshot.png)

Warp is a powerful and versatile cross-platform download manager. With its advanced technology, Warp has the ability to accelerate your download speeds by up to 10 times, revolutionizing the way you obtain files on any operating system.



## Getting Started

Although WarpDL can be installed using various package managers, but you can also build it manually.

### Prerequisites

You will need the following things for building warpdl binary:

* This Repository - clone it using the following command:
   ```git clone https://github.com/warpdl/warpdl```
* Go v1.18+ - You can download it from [go.dev/dl](https://go.dev/dl).

### Installation

- Building form source:

  1. Run the following command in the repo directory of warpdl:
      ```go mod tidy```
  
  2. Build the daemon and cli using standard go build command:
      ```go build -ldflags="-s -w" ./cmd/warpd```
      ```go build -ldflags="-s -w" ./cmd/warpdl```
  
  3. Add the binary to `PATH` environment variable.

- Installing through package managers:
  - Scoop (Windows):
      ```
      scoop bucket add warpdl https://github.com/warpdl/scoop-bucket
      scoop install warpdl
      ```
  - Homebrew:
      ```
      brew install warpdl/tap/warpdl
      ```
- Installing through official bash script:
  ```
  curl -fsSL https://raw.githubusercontent.com/warpdl/warpdl/dev/scripts/install.sh | sh
  ```
- Other

  You can download all binaries and release artifacts from the [Releases](https://github.com/warpdl/warpdl/releases/latest) page. Binaries are built for macOS, Linux, Windows, FreeBSD, OpenBSD, and NetBSD, and for 32-bit, 64-bit, armv6/armv7, and armv6/armv7 64-bit architectures.

  If a binary does not yet exist for the OS/architecture you use, please open a GitHub Issue.
## Usage

Use `warpdl help <command>` for information about various commands.

## Roadmap

See the [open issues](https://github.com/warpdl/warpdl/issues) for a list of proposed features (and known issues).

## Contributing

Contributions are what make the open source community such an amazing place to be learn, inspire, and create. Any contributions you make are **greatly appreciated**.
* If you have suggestions for adding or removing features, feel free to [open an issue](https://github.com/warpdl/warpdl/issues/new) to discuss it, or directly create a pull request after you edit the *README.md* file with necessary changes.
* Please make sure you check your spelling and grammar.
* Create individual PR for each suggestion.

### Creating A Pull Request

1. Fork the Project
2. Create your Feature Branch (`git checkout -b feature/AmazingFeature`)
3. Commit your Changes (`git commit -m 'Add some AmazingFeature'`)
4. Push to the Branch (`git push origin feature/AmazingFeature`)
5. Open a Pull Request

## License

Distributed under the MIT License. See [LICENSE](https://github.com/warpdl/warpdl/blob/dev/LICENSE) for more information.

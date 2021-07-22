package dockerfile

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/model"
)

func testInstallCog(generatedPaths []string) string {
	return `RUN ### --> Installing Cog
COPY .cog/tmp/cog-0.0.1.dev-py3-none-any.whl /tmp/cog-0.0.1.dev-py3-none-any.whl
RUN pip install /tmp/cog-0.0.1.dev-py3-none-any.whl`
}

func testInstallPython(version string) string {
	return fmt.Sprintf(`RUN ### --> Installing Python prerequisites
ENV PATH="/root/.pyenv/shims:/root/.pyenv/bin:$PATH"
RUN apt-get update -q && apt-get install -qy --no-install-recommends \
	make \
	build-essential \
	libssl-dev \
	zlib1g-dev \
	libbz2-dev \
	libreadline-dev \
	libsqlite3-dev \
	wget \
	curl \
	llvm \
	libncurses5-dev \
	libncursesw5-dev \
	xz-utils \
	tk-dev \
	libffi-dev \
	liblzma-dev \
	python-openssl \
	git \
	ca-certificates \
	&& rm -rf /var/lib/apt/lists/*
RUN ### --> Installing Python 3.8
RUN curl https://pyenv.run | bash && \
	git clone https://github.com/momo-lab/pyenv-install-latest.git "$(pyenv root)"/plugins/pyenv-install-latest && \
	pyenv install-latest "%s" && \
	pyenv global $(pyenv install-latest --print "%s")
`, version, version)
}

func TestGenerateEmpty(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test")
	require.NoError(t, err)

	conf, err := model.ConfigFromYAML([]byte(`
model: predict.py:Model
`))
	require.NoError(t, err)
	require.NoError(t, conf.ValidateAndCompleteConfig())

	gen := DockerfileGenerator{Config: conf, Arch: "cpu", Dir: tmpDir}
	actualCPU, err := gen.Generate()
	require.NoError(t, err)

	expectedCPU := `FROM python:3.8
ENV DEBIAN_FRONTEND=noninteractive
ENV PYTHONUNBUFFERED=1
ENV LD_LIBRARY_PATH=$LD_LIBRARY_PATH:/usr/lib/x86_64-linux-gnu
` + testInstallCog(gen.generatedPaths) + `
WORKDIR /src
CMD ["python", "-m", "cog.server.http"]
RUN ### --> Copying code
COPY . /src`

	gen = DockerfileGenerator{Config: conf, Arch: "gpu", Dir: tmpDir}
	actualGPU, err := gen.Generate()
	require.NoError(t, err)

	expectedGPU := `FROM nvidia/cuda:11.0-cudnn8-devel-ubuntu16.04
ENV DEBIAN_FRONTEND=noninteractive
ENV PYTHONUNBUFFERED=1
ENV LD_LIBRARY_PATH=$LD_LIBRARY_PATH:/usr/lib/x86_64-linux-gnu
` + testInstallPython("3.8") + testInstallCog(gen.generatedPaths) + `
WORKDIR /src
CMD ["python", "-m", "cog.server.http"]
RUN ### --> Copying code
COPY . /src`

	require.Equal(t, expectedCPU, actualCPU)
	require.Equal(t, expectedGPU, actualGPU)
}

func TestGenerateFull(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test")
	require.NoError(t, err)

	conf, err := model.ConfigFromYAML([]byte(`
environment:
  python_requirements: my-requirements.txt
  python_packages:
    - torch==1.5.1
    - pandas==1.2.0.12
  system_packages:
    - ffmpeg
    - cowsay
model: predict.py:Model
`))
	require.NoError(t, err)
	require.NoError(t, conf.ValidateAndCompleteConfig())

	gen := DockerfileGenerator{Config: conf, Arch: "cpu", Dir: tmpDir}
	actualCPU, err := gen.Generate()
	require.NoError(t, err)

	expectedCPU := `FROM python:3.8
ENV DEBIAN_FRONTEND=noninteractive
ENV PYTHONUNBUFFERED=1
ENV LD_LIBRARY_PATH=$LD_LIBRARY_PATH:/usr/lib/x86_64-linux-gnu
` + testInstallCog(gen.generatedPaths) + `
RUN ### --> Installing system packages
RUN apt-get update -qq && apt-get install -qy ffmpeg cowsay && rm -rf /var/lib/apt/lists/*
RUN ### --> Installing Python requirements
COPY my-requirements.txt /tmp/requirements.txt
RUN pip install -r /tmp/requirements.txt && rm /tmp/requirements.txt
RUN ### --> Installing Python packages
RUN pip install -f https://download.pytorch.org/whl/torch_stable.html   torch==1.5.1+cpu pandas==1.2.0.12
WORKDIR /src
CMD ["python", "-m", "cog.server.http"]
RUN ### --> Copying code
COPY . /src`

	gen = DockerfileGenerator{Config: conf, Arch: "gpu", Dir: tmpDir}
	actualGPU, err := gen.Generate()
	require.NoError(t, err)

	expectedGPU := `FROM nvidia/cuda:10.2-cudnn8-devel-ubuntu18.04
ENV DEBIAN_FRONTEND=noninteractive
ENV PYTHONUNBUFFERED=1
ENV LD_LIBRARY_PATH=$LD_LIBRARY_PATH:/usr/lib/x86_64-linux-gnu
` + testInstallPython("3.8") +
		testInstallCog(gen.generatedPaths) + `
RUN ### --> Installing system packages
RUN apt-get update -qq && apt-get install -qy ffmpeg cowsay && rm -rf /var/lib/apt/lists/*
RUN ### --> Installing Python requirements
COPY my-requirements.txt /tmp/requirements.txt
RUN pip install -r /tmp/requirements.txt && rm /tmp/requirements.txt
RUN ### --> Installing Python packages
RUN pip install   torch==1.5.1 pandas==1.2.0.12
WORKDIR /src
CMD ["python", "-m", "cog.server.http"]
RUN ### --> Copying code
COPY . /src`

	require.Equal(t, expectedCPU, actualCPU)
	require.Equal(t, expectedGPU, actualGPU)
}

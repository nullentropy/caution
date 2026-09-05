FROM golang:1.26-trixie

RUN apt-get update && apt-get install -y --no-install-recommends \
      pkg-config \
      libgl1-mesa-dev libx11-dev libxcursor-dev libxi-dev \
      libxinerama-dev libxrandr-dev libxxf86vm-dev \
      libwayland-dev libxkbcommon-dev wayland-protocols \
      libgl1-mesa-dri libglx-mesa0 libegl1-mesa-dev \
      libasound2-dev \
      xvfb xauth mesa-utils chromium nodejs fonts-dejavu-core \
      desktop-file-utils x11-utils \
      bc \
 && rm -rf /var/lib/apt/lists/*

ENV LIBGL_ALWAYS_SOFTWARE=1 \
    GALLIUM_DRIVER=llvmpipe \
    CHROME_BIN=/usr/bin/chromium \
    GOFLAGS=-buildvcs=false

WORKDIR /caution

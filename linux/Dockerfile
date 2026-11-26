FROM golang:1.26-trixie

# libgl1-mesa-dev only satisfies go-gl/glfw's GL/gl.h include and -lGL flag
# --as-needed drops libGL from the binary
RUN apt-get update && apt-get install -y --no-install-recommends \
      pkg-config \
      libvulkan-dev mesa-vulkan-drivers vulkan-tools vulkan-validationlayers \
      libgl1-mesa-dev \
      libx11-dev libxcursor-dev libxi-dev \
      libxinerama-dev libxrandr-dev libxxf86vm-dev \
      libwayland-dev libxkbcommon-dev wayland-protocols \
      libasound2-dev \
      xvfb xauth chromium nodejs fonts-dejavu-core \
      desktop-file-utils x11-utils imagemagick xdotool \
      binutils bc \
 && rm -rf /var/lib/apt/lists/*

ENV CHROME_BIN=/usr/bin/chromium \
    GOFLAGS=-buildvcs=false

WORKDIR /caution

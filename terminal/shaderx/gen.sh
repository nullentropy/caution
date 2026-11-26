#!/bin/sh
cd "$(dirname "$0")"
rm -f zz_*.cpp
find third_party -name '*.cpp' | sort | while read f; do
	n=$(echo "$f" | sed -e 's|third_party/||' -e 's|/|_|g' -e 's|\.cpp$||')
	case "$f" in
	*OSDependent/Unix/*) n="${n}_unix" ;;
	*OSDependent/Windows/*) n="${n}_windows" ;;
	esac
	case "$n" in *_unix) printf "//go:build unix\n" > "zz_$n.cpp" ;; *) : > "zz_$n.cpp" ;; esac
	echo "#include \"$f\"" >> "zz_$n.cpp"
done

UPDATE languages
SET compile_command = '/usr/bin/g++ -B/usr/bin -O2 -std=c++17 -fsanitize=undefined -fno-sanitize-recover=all -o main %s main.cpp'
WHERE name = 'C++' AND version = 'GCC 14.2.0';

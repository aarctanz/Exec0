UPDATE languages
SET compile_command = '/usr/bin/g++ -B/usr/bin -o main %s main.cpp'
WHERE name = 'C++' AND version = 'GCC 14.2.0';

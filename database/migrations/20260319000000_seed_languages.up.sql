INSERT INTO languages (name, version, source_file, compile_command, run_command) VALUES
    ('C++', 'GCC 14.2.0', 'main.cpp', '/usr/bin/g++ -B/usr/bin -o main %s main.cpp', './main'),
    ('Java', 'OpenJDK 23', 'Main.java', '/usr/bin/javac %s Main.java', '/usr/bin/java Main'),
    ('Python', '3.13', 'script.py', NULL, '/usr/bin/python3 script.py')
ON CONFLICT (name, version) DO NOTHING;

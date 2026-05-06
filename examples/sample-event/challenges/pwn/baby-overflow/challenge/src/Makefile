CC      := gcc
CFLAGS  := -fno-stack-protector -no-pie -z execstack -m64 -O0

chal: chal.c
	$(CC) $(CFLAGS) chal.c -o chal

clean:
	rm -f chal

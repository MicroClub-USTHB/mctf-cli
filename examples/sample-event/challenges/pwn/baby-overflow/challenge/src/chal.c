#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

void win(void) {
    char buf[128];
    FILE *f = fopen("/srv/app/flag.txt", "r");
    if (!f) { puts("flag missing"); exit(1); }
    fgets(buf, sizeof(buf), f);
    fputs(buf, stdout);
    fclose(f);
}

void greet(void) {
    char name[64];
    setbuf(stdout, NULL);
    printf("name? ");
    gets(name);                // intentional: classic bof
    printf("hi, %s\n", name);
}

int main(void) {
    greet();
    return 0;
}

#ifndef JUST_TALK_PLUS_CLIPBOARD_DARWIN_H
#define JUST_TALK_PLUS_CLIPBOARD_DARWIN_H

#include <stddef.h>
#include <stdint.h>

int jt_clipboard_set(const char *text);
char *jt_clipboard_get(void);
char *jt_clipboard_types(void);
int jt_clipboard_item_count(void);
int jt_clipboard_read_data(const char *type, void **data, size_t *length);
int jt_clipboard_clear(void);
int jt_clipboard_set_data(const char *type, const void *data, size_t length);

#endif

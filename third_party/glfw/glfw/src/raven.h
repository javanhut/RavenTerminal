//========================================================================
// Raven Terminal extensions to GLFW 3.3: platform-independent helpers
//------------------------------------------------------------------------
// Kept free of GLFW internals so they can be unit tested on their own.
//========================================================================

#ifndef _glfw3_raven_h_
#define _glfw3_raven_h_

#include <stdint.h>
#include <stdlib.h>
#include <string.h>

// Wayland fractional scales are expressed in 120ths. Buffer sizes round half
// away from zero, as wp_fractional_scale_v1 recommends.
static inline int _glfwRavenScaledSize(int logical, int scale120)
{
    if (scale120 <= 0)
        scale120 = 120;
    return (int) (((int64_t) logical * scale120 + 60) / 120);
}

static inline float _glfwRavenScaleFromWire(uint32_t scale120)
{
    return scale120 ? scale120 / 120.f : 1.f;
}

#if defined(__unix__) || defined(__APPLE__)
// Returns a malloc'd copy of the named environment variable, or NULL when it
// is unset or empty, and removes it so child processes do not inherit it
static inline char* _glfwRavenTakeEnv(const char* name)
{
    const char* value = getenv(name);
    char* copy = NULL;

    if (!value)
        return NULL;

    if (*value)
    {
        const size_t length = strlen(value);
        copy = malloc(length + 1);
        if (copy)
            memcpy(copy, value, length + 1);
    }

    unsetenv(name);
    return copy;
}
#endif

// Decodes one UTF-8 sequence and advances the string; malformed input yields
// U+FFFD and consumes one byte
static inline uint32_t _glfwRavenDecodeUTF8(const char** string)
{
    const unsigned char* s = (const unsigned char*) *string;
    uint32_t cp;
    int count, i;

    if (s[0] < 0x80)
    {
        *string += 1;
        return s[0];
    }
    else if ((s[0] & 0xe0) == 0xc0)
        cp = s[0] & 0x1f, count = 1;
    else if ((s[0] & 0xf0) == 0xe0)
        cp = s[0] & 0x0f, count = 2;
    else if ((s[0] & 0xf8) == 0xf0)
        cp = s[0] & 0x07, count = 3;
    else
    {
        *string += 1;
        return 0xfffd;
    }

    for (i = 1;  i <= count;  i++)
    {
        if ((s[i] & 0xc0) != 0x80)
        {
            *string += 1;
            return 0xfffd;
        }
        cp = (cp << 6) | (s[i] & 0x3f);
    }

    *string += count + 1;
    return cp;
}

// Returns the byte offset of the given codepoint index, clamped to the string
static inline int _glfwRavenUTF8ByteOffset(const char* string, int codepoints)
{
    const char* s = string;

    while (codepoints-- > 0 && *s)
        _glfwRavenDecodeUTF8(&s);

    return (int) (s - string);
}

// Replaces changeLength codepoints at first with insert (X11 preedit draw
// semantics, out-of-range edits clamped); returns the new length, or -1 if
// memory ran out
static inline int _glfwRavenPreeditEdit(uint32_t** buffer, int length,
                                        int first, int changeLength,
                                        const uint32_t* insert, int insertLength)
{
    uint32_t* buf;
    int newLength;

    if (first < 0)
        first = 0;
    if (first > length)
        first = length;
    if (changeLength < 0)
        changeLength = 0;
    if (changeLength > length - first)
        changeLength = length - first;
    if (insertLength < 0)
        insertLength = 0;

    newLength = length - changeLength + insertLength;
    buf = realloc(*buffer, ((newLength > length ? newLength : length) + 1) *
                           sizeof(uint32_t));
    if (!buf)
        return -1;

    memmove(buf + first + insertLength,
            buf + first + changeLength,
            (length - first - changeLength) * sizeof(uint32_t));
    if (insertLength)
        memcpy(buf + first, insert, insertLength * sizeof(uint32_t));

    *buffer = buf;
    return newLength;
}

#endif // _glfw3_raven_h_

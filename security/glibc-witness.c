/* Synthetic regression for CVE-2026-84243, using only public gettext APIs.
 * Copyright 2026 Aeontra contributors. SPDX-License-Identifier: Apache-2.0 */
#include <libintl.h>
#include <locale.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

int main(int argc, char **argv) {
    if (argc != 3) return 2;
    int baseline = strcmp(argv[2], "baseline") == 0;
    char locales[4096], domain[4096], localedir[4096], msgdir[4096];
    if (snprintf(locales, sizeof locales, "%s/locales", argv[1]) >= (int)sizeof locales ||
        snprintf(domain, sizeof domain, "%s/catalog", argv[1]) >= (int)sizeof domain ||
        snprintf(localedir, sizeof localedir, "%s/de_DE", domain) >= (int)sizeof localedir ||
        snprintf(msgdir, sizeof msgdir, "%s/LC_MESSAGES", localedir) >= (int)sizeof msgdir) return 2;
    if (setenv("LOCPATH", locales, 1) || !setlocale(LC_ALL, "de_DE.UTF-8")) return 3;
    unsetenv("OUTPUT_CHARSET");
    textdomain("aeontra-probe");
    struct { const char *directory, *language; int translation; } cases[] = {
        {domain, "de_DE", 1},
        {msgdir, "..", baseline},
        {localedir, "../de_DE", baseline},
        {msgdir, "../../de_DE", baseline},
        {domain, "..:../de_DE:de_DE", 1},
        {localedir, "LC_MESSAGES/..", baseline}
    };
    for (size_t i = 0; i < sizeof cases / sizeof cases[0]; i++) {
        if (!bindtextdomain("aeontra-probe", cases[i].directory) || setenv("LANGUAGE", cases[i].language, 1)) return 4;
        const char *actual = gettext("AEONTRA_TRANSLATION_PROBE");
        const char *expected = cases[i].translation ? "translated" : "AEONTRA_TRANSLATION_PROBE";
        if (strcmp(actual, expected)) { fprintf(stderr, "gettext control %zu failed\n", i); return 5; }
    }
    puts(baseline ? "glibc_baseline_reproduced=4 normal_controls=2" : "glibc_traversal_denied=4 normal_controls=2");
    return 0;
}

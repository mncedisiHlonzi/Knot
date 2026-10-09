-- 0011_iso_639_3 — widen every stored language from ISO 639-1 (2 letters) to ISO 639-3 (3 letters).
--
-- KNOT-015d stored ISO 639-1 codes. KNOT-015d-fix moves the contract to ISO 639-3,
-- which names every language rather than only the 184 that happen to have a
-- two-letter code (KNOT-ADR-046). The service layer now rejects a two-letter code,
-- so existing rows must be rewritten here rather than left to fail validation the
-- next time they are written.
--
-- Four columns hold a language, and all four are rewritten below:
--
--   story_versions.language, comments.language, bridges.target_language,
--   users.preferred_languages
--
-- `stories` is deliberately not among them: migration 0003 dropped
-- `stories.language` when story content moved into a root version, and this
-- migration does not resurrect it.
--
-- The map is the 184 ISO 639-1 codes that have an ISO 639-3 counterpart, taken
-- from the `Part1` column of the SIL International reference table by
-- tools/generate_languages.py, so it agrees with the canonical list by
-- construction. It is injective — no two codes share a target — so this rewrite
-- cannot violate the unique index `bridges_one_per_target_language`.
--
-- Idempotent by construction: every statement matches only values that are
-- exactly two characters, so a second run changes nothing. A two-letter value with
-- no counterpart is left as it is rather than guessed at: inventing data is not
-- this migration's job.

-- The map is needed by four statements, so it lives in a temporary table rather
-- than being repeated as a CASE expression four times. The table is dropped again
-- in the same transaction, so nothing permanent is left behind. The DROP before
-- the CREATE makes the file safe to re-run by hand, where each statement commits
-- on its own.
DROP TABLE IF EXISTS knot_iso639_1_to_3;

CREATE TEMP TABLE knot_iso639_1_to_3 (
    iso639_1 TEXT PRIMARY KEY,
    iso639_3 TEXT NOT NULL UNIQUE
);

INSERT INTO knot_iso639_1_to_3 (iso639_1, iso639_3) VALUES
    ('aa', 'aar'),
    ('ab', 'abk'),
    ('ae', 'ave'),
    ('af', 'afr'),
    ('ak', 'aka'),
    ('am', 'amh'),
    ('an', 'arg'),
    ('ar', 'ara'),
    ('as', 'asm'),
    ('av', 'ava'),
    ('ay', 'aym'),
    ('az', 'aze'),
    ('ba', 'bak'),
    ('be', 'bel'),
    ('bg', 'bul'),
    ('bi', 'bis'),
    ('bm', 'bam'),
    ('bn', 'ben'),
    ('bo', 'bod'),
    ('br', 'bre'),
    ('bs', 'bos'),
    ('ca', 'cat'),
    ('ce', 'che'),
    ('ch', 'cha'),
    ('co', 'cos'),
    ('cr', 'cre'),
    ('cs', 'ces'),
    ('cu', 'chu'),
    ('cv', 'chv'),
    ('cy', 'cym'),
    ('da', 'dan'),
    ('de', 'deu'),
    ('dv', 'div'),
    ('dz', 'dzo'),
    ('ee', 'ewe'),
    ('el', 'ell'),
    ('en', 'eng'),
    ('eo', 'epo'),
    ('es', 'spa'),
    ('et', 'est'),
    ('eu', 'eus'),
    ('fa', 'fas'),
    ('ff', 'ful'),
    ('fi', 'fin'),
    ('fj', 'fij'),
    ('fo', 'fao'),
    ('fr', 'fra'),
    ('fy', 'fry'),
    ('ga', 'gle'),
    ('gd', 'gla'),
    ('gl', 'glg'),
    ('gn', 'grn'),
    ('gu', 'guj'),
    ('gv', 'glv'),
    ('ha', 'hau'),
    ('he', 'heb'),
    ('hi', 'hin'),
    ('ho', 'hmo'),
    ('hr', 'hrv'),
    ('ht', 'hat'),
    ('hu', 'hun'),
    ('hy', 'hye'),
    ('hz', 'her'),
    ('ia', 'ina'),
    ('id', 'ind'),
    ('ie', 'ile'),
    ('ig', 'ibo'),
    ('ii', 'iii'),
    ('ik', 'ipk'),
    ('io', 'ido'),
    ('is', 'isl'),
    ('it', 'ita'),
    ('iu', 'iku'),
    ('ja', 'jpn'),
    ('jv', 'jav'),
    ('ka', 'kat'),
    ('kg', 'kon'),
    ('ki', 'kik'),
    ('kj', 'kua'),
    ('kk', 'kaz'),
    ('kl', 'kal'),
    ('km', 'khm'),
    ('kn', 'kan'),
    ('ko', 'kor'),
    ('kr', 'kau'),
    ('ks', 'kas'),
    ('ku', 'kur'),
    ('kv', 'kom'),
    ('kw', 'cor'),
    ('ky', 'kir'),
    ('la', 'lat'),
    ('lb', 'ltz'),
    ('lg', 'lug'),
    ('li', 'lim'),
    ('ln', 'lin'),
    ('lo', 'lao'),
    ('lt', 'lit'),
    ('lu', 'lub'),
    ('lv', 'lav'),
    ('mg', 'mlg'),
    ('mh', 'mah'),
    ('mi', 'mri'),
    ('mk', 'mkd'),
    ('ml', 'mal'),
    ('mn', 'mon'),
    ('mr', 'mar'),
    ('ms', 'msa'),
    ('mt', 'mlt'),
    ('my', 'mya'),
    ('na', 'nau'),
    ('nb', 'nob'),
    ('nd', 'nde'),
    ('ne', 'nep'),
    ('ng', 'ndo'),
    ('nl', 'nld'),
    ('nn', 'nno'),
    ('no', 'nor'),
    ('nr', 'nbl'),
    ('nv', 'nav'),
    ('ny', 'nya'),
    ('oc', 'oci'),
    ('oj', 'oji'),
    ('om', 'orm'),
    ('or', 'ori'),
    ('os', 'oss'),
    ('pa', 'pan'),
    ('pi', 'pli'),
    ('pl', 'pol'),
    ('ps', 'pus'),
    ('pt', 'por'),
    ('qu', 'que'),
    ('rm', 'roh'),
    ('rn', 'run'),
    ('ro', 'ron'),
    ('ru', 'rus'),
    ('rw', 'kin'),
    ('sa', 'san'),
    ('sc', 'srd'),
    ('sd', 'snd'),
    ('se', 'sme'),
    ('sg', 'sag'),
    ('sh', 'hbs'),
    ('si', 'sin'),
    ('sk', 'slk'),
    ('sl', 'slv'),
    ('sm', 'smo'),
    ('sn', 'sna'),
    ('so', 'som'),
    ('sq', 'sqi'),
    ('sr', 'srp'),
    ('ss', 'ssw'),
    ('st', 'sot'),
    ('su', 'sun'),
    ('sv', 'swe'),
    ('sw', 'swa'),
    ('ta', 'tam'),
    ('te', 'tel'),
    ('tg', 'tgk'),
    ('th', 'tha'),
    ('ti', 'tir'),
    ('tk', 'tuk'),
    ('tl', 'tgl'),
    ('tn', 'tsn'),
    ('to', 'ton'),
    ('tr', 'tur'),
    ('ts', 'tso'),
    ('tt', 'tat'),
    ('tw', 'twi'),
    ('ty', 'tah'),
    ('ug', 'uig'),
    ('uk', 'ukr'),
    ('ur', 'urd'),
    ('uz', 'uzb'),
    ('ve', 'ven'),
    ('vi', 'vie'),
    ('vo', 'vol'),
    ('wa', 'wln'),
    ('wo', 'wol'),
    ('xh', 'xho'),
    ('yi', 'yid'),
    ('yo', 'yor'),
    ('za', 'zha'),
    ('zh', 'zho'),
    ('zu', 'zul');

-- Scalar columns: a plain join on the map, deterministic because iso639_1 is the
-- primary key, so at most one map row can match a stored value.

UPDATE story_versions
SET language = mapped.iso639_3
FROM knot_iso639_1_to_3 mapped
WHERE story_versions.language = mapped.iso639_1;

UPDATE comments
SET language = mapped.iso639_3
FROM knot_iso639_1_to_3 mapped
WHERE comments.language = mapped.iso639_1;

UPDATE bridges
SET target_language = mapped.iso639_3
FROM knot_iso639_1_to_3 mapped
WHERE bridges.target_language = mapped.iso639_1;

-- An array element cannot be joined in place, so each element is looked up and the
-- array is re-aggregated in its original order. Only users holding at least one
-- two-letter value are touched, so an empty array is never rewritten into NULL and
-- rows that need no change are not written at all.
UPDATE users
SET preferred_languages = (
    SELECT COALESCE(array_agg(COALESCE(mapped.iso639_3, element)), ARRAY[]::TEXT[])
    FROM unnest(users.preferred_languages) AS element
    LEFT JOIN knot_iso639_1_to_3 mapped ON mapped.iso639_1 = element
)
WHERE EXISTS (
    SELECT 1
    FROM unnest(users.preferred_languages) AS element
    WHERE length(element) = 2
);

DROP TABLE knot_iso639_1_to_3;

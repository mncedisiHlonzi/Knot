-- 0011_iso_639_3 (down) — best-effort reversal to ISO 639-1.
--
-- This migration is deliberately lossy in the only direction that matters: it
-- reverses the 184 codes that have a two-letter counterpart and leaves every other
-- three-letter code exactly as it is. Nothing is deleted and no field is nulled,
-- so `nso` (Sepedi) survives as `nso` — a database that has been through up and
-- back down holds a mix of two- and three-letter codes, which is the honest
-- outcome when information the down path needs was never recorded.
--
-- Reversing also undoes codes that were three letters to begin with, because a
-- stored code carries no record of which migration wrote it. Recovering exact
-- provenance would need a history column, which is not worth a schema change for a
-- rollback path that should rarely run.
--
-- Like the up migration, this matches only values that are exactly three
-- characters, so it is idempotent and cannot touch a value it has already
-- reversed. The reverse map is injective, so it cannot violate
-- `bridges_one_per_target_language` either.

DROP TABLE IF EXISTS knot_iso639_3_to_1;

CREATE TEMP TABLE knot_iso639_3_to_1 (
    iso639_3 TEXT PRIMARY KEY,
    iso639_1 TEXT NOT NULL UNIQUE
);

INSERT INTO knot_iso639_3_to_1 (iso639_3, iso639_1) VALUES
    ('aar', 'aa'),
    ('abk', 'ab'),
    ('afr', 'af'),
    ('aka', 'ak'),
    ('amh', 'am'),
    ('ara', 'ar'),
    ('arg', 'an'),
    ('asm', 'as'),
    ('ava', 'av'),
    ('ave', 'ae'),
    ('aym', 'ay'),
    ('aze', 'az'),
    ('bak', 'ba'),
    ('bam', 'bm'),
    ('bel', 'be'),
    ('ben', 'bn'),
    ('bis', 'bi'),
    ('bod', 'bo'),
    ('bos', 'bs'),
    ('bre', 'br'),
    ('bul', 'bg'),
    ('cat', 'ca'),
    ('ces', 'cs'),
    ('cha', 'ch'),
    ('che', 'ce'),
    ('chu', 'cu'),
    ('chv', 'cv'),
    ('cor', 'kw'),
    ('cos', 'co'),
    ('cre', 'cr'),
    ('cym', 'cy'),
    ('dan', 'da'),
    ('deu', 'de'),
    ('div', 'dv'),
    ('dzo', 'dz'),
    ('ell', 'el'),
    ('eng', 'en'),
    ('epo', 'eo'),
    ('est', 'et'),
    ('eus', 'eu'),
    ('ewe', 'ee'),
    ('fao', 'fo'),
    ('fas', 'fa'),
    ('fij', 'fj'),
    ('fin', 'fi'),
    ('fra', 'fr'),
    ('fry', 'fy'),
    ('ful', 'ff'),
    ('gla', 'gd'),
    ('gle', 'ga'),
    ('glg', 'gl'),
    ('glv', 'gv'),
    ('grn', 'gn'),
    ('guj', 'gu'),
    ('hat', 'ht'),
    ('hau', 'ha'),
    ('hbs', 'sh'),
    ('heb', 'he'),
    ('her', 'hz'),
    ('hin', 'hi'),
    ('hmo', 'ho'),
    ('hrv', 'hr'),
    ('hun', 'hu'),
    ('hye', 'hy'),
    ('ibo', 'ig'),
    ('ido', 'io'),
    ('iii', 'ii'),
    ('iku', 'iu'),
    ('ile', 'ie'),
    ('ina', 'ia'),
    ('ind', 'id'),
    ('ipk', 'ik'),
    ('isl', 'is'),
    ('ita', 'it'),
    ('jav', 'jv'),
    ('jpn', 'ja'),
    ('kal', 'kl'),
    ('kan', 'kn'),
    ('kas', 'ks'),
    ('kat', 'ka'),
    ('kau', 'kr'),
    ('kaz', 'kk'),
    ('khm', 'km'),
    ('kik', 'ki'),
    ('kin', 'rw'),
    ('kir', 'ky'),
    ('kom', 'kv'),
    ('kon', 'kg'),
    ('kor', 'ko'),
    ('kua', 'kj'),
    ('kur', 'ku'),
    ('lao', 'lo'),
    ('lat', 'la'),
    ('lav', 'lv'),
    ('lim', 'li'),
    ('lin', 'ln'),
    ('lit', 'lt'),
    ('ltz', 'lb'),
    ('lub', 'lu'),
    ('lug', 'lg'),
    ('mah', 'mh'),
    ('mal', 'ml'),
    ('mar', 'mr'),
    ('mkd', 'mk'),
    ('mlg', 'mg'),
    ('mlt', 'mt'),
    ('mon', 'mn'),
    ('mri', 'mi'),
    ('msa', 'ms'),
    ('mya', 'my'),
    ('nau', 'na'),
    ('nav', 'nv'),
    ('nbl', 'nr'),
    ('nde', 'nd'),
    ('ndo', 'ng'),
    ('nep', 'ne'),
    ('nld', 'nl'),
    ('nno', 'nn'),
    ('nob', 'nb'),
    ('nor', 'no'),
    ('nya', 'ny'),
    ('oci', 'oc'),
    ('oji', 'oj'),
    ('ori', 'or'),
    ('orm', 'om'),
    ('oss', 'os'),
    ('pan', 'pa'),
    ('pli', 'pi'),
    ('pol', 'pl'),
    ('por', 'pt'),
    ('pus', 'ps'),
    ('que', 'qu'),
    ('roh', 'rm'),
    ('ron', 'ro'),
    ('run', 'rn'),
    ('rus', 'ru'),
    ('sag', 'sg'),
    ('san', 'sa'),
    ('sin', 'si'),
    ('slk', 'sk'),
    ('slv', 'sl'),
    ('sme', 'se'),
    ('smo', 'sm'),
    ('sna', 'sn'),
    ('snd', 'sd'),
    ('som', 'so'),
    ('sot', 'st'),
    ('spa', 'es'),
    ('sqi', 'sq'),
    ('srd', 'sc'),
    ('srp', 'sr'),
    ('ssw', 'ss'),
    ('sun', 'su'),
    ('swa', 'sw'),
    ('swe', 'sv'),
    ('tah', 'ty'),
    ('tam', 'ta'),
    ('tat', 'tt'),
    ('tel', 'te'),
    ('tgk', 'tg'),
    ('tgl', 'tl'),
    ('tha', 'th'),
    ('tir', 'ti'),
    ('ton', 'to'),
    ('tsn', 'tn'),
    ('tso', 'ts'),
    ('tuk', 'tk'),
    ('tur', 'tr'),
    ('twi', 'tw'),
    ('uig', 'ug'),
    ('ukr', 'uk'),
    ('urd', 'ur'),
    ('uzb', 'uz'),
    ('ven', 've'),
    ('vie', 'vi'),
    ('vol', 'vo'),
    ('wln', 'wa'),
    ('wol', 'wo'),
    ('xho', 'xh'),
    ('yid', 'yi'),
    ('yor', 'yo'),
    ('zha', 'za'),
    ('zho', 'zh'),
    ('zul', 'zu');

UPDATE story_versions
SET language = mapped.iso639_1
FROM knot_iso639_3_to_1 mapped
WHERE story_versions.language = mapped.iso639_3;

UPDATE comments
SET language = mapped.iso639_1
FROM knot_iso639_3_to_1 mapped
WHERE comments.language = mapped.iso639_3;

UPDATE bridges
SET target_language = mapped.iso639_1
FROM knot_iso639_3_to_1 mapped
WHERE bridges.target_language = mapped.iso639_3;

UPDATE users
SET preferred_languages = (
    SELECT COALESCE(array_agg(COALESCE(mapped.iso639_1, element)), ARRAY[]::TEXT[])
    FROM unnest(users.preferred_languages) AS element
    LEFT JOIN knot_iso639_3_to_1 mapped ON mapped.iso639_3 = element
)
WHERE EXISTS (
    SELECT 1
    FROM unnest(users.preferred_languages) AS element
    WHERE length(element) = 3
);

DROP TABLE knot_iso639_3_to_1;

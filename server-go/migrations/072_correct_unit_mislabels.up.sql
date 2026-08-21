-- 072: Correct unit mislabels on purchase/expense lines so the Price-Change
--      report stops showing fake 100%–6000% swings.
--
-- Reviewed on the Perubahan Harga page before shipping. Only rows whose price
-- unambiguously belongs to a DIFFERENT unit are moved; prices and quantities
-- are never touched. Suspected price typos (e.g. dropped zeros) are LEFT alone
-- on purpose — they need a value decision, not a unit move, and will be handled
-- separately.
--
-- Two items also gain a second unit so their lines can be expressed correctly:
--   TERIGU CAKRA      + kg   (1 ball = 25 kg)
--   CABE RAWIT MERAH  + ons  (1 kg  = 10 ons)
--
-- Row moves are keyed by invoice_items.id from the reviewed snapshot, so this
-- touches exactly the audited rows and nothing entered afterwards.

-- ── 1. New unit definitions ─────────────────────────────────────────────
UPDATE items SET units = '[{"name":"ball","perPrev":null},{"name":"kg","perPrev":25}]'::jsonb WHERE id = 'ab18b254-cde8-4072-a733-2ead03d22089';  -- TERIGU CAKRA: ball + kg
UPDATE items SET units = '[{"name":"kg","perPrev":null},{"name":"ons","perPrev":10}]'::jsonb WHERE id = 'fcc611ff-932b-49c8-a3f3-34d4b86ca023';  -- CABE RAWIT MERAH: kg + ons

-- ── 2. TERIGU CAKRA: ball-priced rows sitting on the orphan index → ball(0) ──
UPDATE invoice_items SET unit_index = 0 WHERE id = '6769288c-b8f3-46ce-b421-4cd77006befb';  -- INV-02642 2026-07-06 19 @ 221,900
UPDATE invoice_items SET unit_index = 0 WHERE id = '3ee2f35b-0baf-4a4f-84d3-07e83a8db97b';  -- INV-03058 2026-07-14 2 @ 221,900
UPDATE invoice_items SET unit_index = 0 WHERE id = 'c71e788b-8746-41fb-8ed2-e4322da9f715';  -- INV-03041 2026-07-17 1 @ 221,900
UPDATE invoice_items SET unit_index = 0 WHERE id = '2246600f-8efe-4311-aa70-ffbe58ff5fec';  -- INV-03433 2026-07-23 1 @ 221,900
UPDATE invoice_items SET unit_index = 0 WHERE id = 'b74e3b14-84fd-4889-a074-061f7d9a1eaf';  -- INV-03063 2026-07-15 2 @ 221,900
UPDATE invoice_items SET unit_index = 0 WHERE id = '4c7a4ec8-efaa-4d61-be7f-4c1684ae3546';  -- INV-02838 2026-07-10 3 @ 221,900
UPDATE invoice_items SET unit_index = 0 WHERE id = 'b53cd983-44ec-4555-806b-1f61bca8d4b4';  -- INV-02971 2026-07-14 25 @ 221,900
UPDATE invoice_items SET unit_index = 0 WHERE id = '594843e9-69b2-4b8e-8130-d815c6e814ea';  -- INV-02846 2026-07-11 2 @ 221,900
UPDATE invoice_items SET unit_index = 0 WHERE id = '0ae37692-c154-4c04-8fb7-e3b38a3b3ea8';  -- INV-02848 2026-07-11 2 @ 221,900
UPDATE invoice_items SET unit_index = 0 WHERE id = 'f28035f0-779b-4d7c-aea5-5bbd9bbbb6ff';  -- INV-02852 2026-07-12 1 @ 221,900
UPDATE invoice_items SET unit_index = 0 WHERE id = '00dd292d-320e-4553-8b5a-235b6161d265';  -- INV-02853 2026-07-12 2 @ 221,900
UPDATE invoice_items SET unit_index = 0 WHERE id = 'fd419c18-d873-4152-abf4-fb662af951bb';  -- INV-02859 2026-07-13 1 @ 221,900
UPDATE invoice_items SET unit_index = 0 WHERE id = 'ce341712-b202-4350-b68e-5254d53050d7';  -- INV-02860 2026-07-13 2 @ 221,900
UPDATE invoice_items SET unit_index = 0 WHERE id = 'caa3b65c-5497-4858-b5c1-df686e15fa20';  -- INV-02990 2026-07-15 1 @ 221,900
UPDATE invoice_items SET unit_index = 0 WHERE id = 'ae94c53c-ba59-4d05-9773-3acf1ff89094';  -- INV-03071 2026-07-16 2 @ 221,900
UPDATE invoice_items SET unit_index = 0 WHERE id = '9351205d-da6c-41f6-9aa6-40f26ad4d42f';  -- INV-03586 2026-07-25 2 @ 221,900
UPDATE invoice_items SET unit_index = 0 WHERE id = '60bc7ec2-ac4b-4247-94a0-ceac27efb2b2';  -- INV-03077 2026-07-17 2 @ 221,900
UPDATE invoice_items SET unit_index = 0 WHERE id = '11dde5f5-e9c4-4b36-96ca-fc380582aed4';  -- INV-03076 2026-07-17 3 @ 221,900
UPDATE invoice_items SET unit_index = 0 WHERE id = 'a4f4a7b1-50c6-4334-8473-1c1c6ddd815b';  -- INV-03463 2026-07-20 3 @ 221,900
UPDATE invoice_items SET unit_index = 0 WHERE id = '26874a4d-b915-4ce3-a20d-2d3da9574aa4';  -- INV-03222 2026-07-18 4 @ 221,900
UPDATE invoice_items SET unit_index = 0 WHERE id = '64cdfad8-ea70-4c8d-ba40-f086e64089f0';  -- INV-03221 2026-07-18 2 @ 221,900
UPDATE invoice_items SET unit_index = 0 WHERE id = 'f068c5c3-de99-4ae3-a3cc-6e46cb8b31c5';  -- INV-03228 2026-07-19 1 @ 221,900
UPDATE invoice_items SET unit_index = 0 WHERE id = '6a00568a-f39e-4f68-848d-2050bb4ca0bb';  -- INV-03282 2026-07-20 1 @ 221,900
UPDATE invoice_items SET unit_index = 0 WHERE id = '184729f3-17d9-48d9-a4bd-76a71e3b9c94';  -- INV-03593 2026-07-26 2 @ 221,900
UPDATE invoice_items SET unit_index = 0 WHERE id = '1ec95edf-9f38-4111-abc0-cf7150a5b661';  -- INV-03595 2026-07-26 3 @ 221,900

-- ── 3. Packaging & MSG: rows whose price is really the OTHER unit ───────────
-- TONGCAI RRC
UPDATE invoice_items SET unit_index = 0 WHERE id = 'ad41f060-f9a0-4f74-a8f3-ca318614b48b';  -- INV-0885 2026-06-01 60 @ 7,167 (pcs->dus)
UPDATE invoice_items SET unit_index = 0 WHERE id = '81404bd4-c71e-482a-b8af-b626b538848a';  -- INV-01694 2026-06-22 60 @ 7,167 (pcs->dus)
UPDATE invoice_items SET unit_index = 0 WHERE id = 'e0414792-a28b-4a96-a6ee-08ae7070e033';  -- INV-02072 2026-06-30 60 @ 7,167 (pcs->dus)
UPDATE invoice_items SET unit_index = 0 WHERE id = '8f6bb555-2d50-4d0b-8d64-04fd66e8fd31';  -- INV-01906 2026-07-01 14 @ 7,166 (pcs->dus)
UPDATE invoice_items SET unit_index = 0 WHERE id = '99ac899a-c36f-4be8-aaa6-7a042c150a71';  -- INV-01979 2026-07-02 10 @ 7,167 (pcs->dus)
UPDATE invoice_items SET unit_index = 0 WHERE id = 'ce7c2103-ca1f-421e-b58d-aac13be37097';  -- INV-02179 2026-07-03 5 @ 7,167 (pcs->dus)
UPDATE invoice_items SET unit_index = 0 WHERE id = 'b9555192-75db-4833-9435-a2756412f7af';  -- INV-02339 2026-07-04 10 @ 7,167 (pcs->dus)
UPDATE invoice_items SET unit_index = 0 WHERE id = '0dce3a99-3a2b-496a-8755-940c85d38d98';  -- INV-02508 2026-07-05 55 @ 7,167 (pcs->dus)
UPDATE invoice_items SET unit_index = 0 WHERE id = '7738a1af-2b59-4188-85ce-ced62a46044b';  -- INV-02452 2026-07-05 5 @ 7,167 (pcs->dus)
UPDATE invoice_items SET unit_index = 0 WHERE id = '6c54b834-e85e-4c17-a77e-24e62a2c9744';  -- INV-02818 2026-07-07 5 @ 7,167 (pcs->dus)
UPDATE invoice_items SET unit_index = 0 WHERE id = '8a978e60-b830-452c-b181-30e6893afa6e';  -- INV-02688 2026-07-08 5 @ 7,167 (pcs->dus)
UPDATE invoice_items SET unit_index = 0 WHERE id = '3f97b4c4-8d23-4c2b-b060-b17bdfbdb73e';  -- INV-02732 2026-07-10 10 @ 7,167 (pcs->dus)
UPDATE invoice_items SET unit_index = 0 WHERE id = 'c4e6c185-d21a-4603-acd3-f6d06d4e3582';  -- INV-02953 2026-07-13 5 @ 7,167 (pcs->dus)
UPDATE invoice_items SET unit_index = 0 WHERE id = 'e6e05667-242b-4549-b0e8-16f661024d75';  -- INV-02960 2026-07-14 10 @ 7,167 (pcs->dus)
UPDATE invoice_items SET unit_index = 0 WHERE id = '71f33098-9041-42c7-9a43-8772ad2fd22e';  -- INV-02971 2026-07-14 60 @ 7,167 (pcs->dus)
UPDATE invoice_items SET unit_index = 0 WHERE id = 'e46a3721-bc02-4d32-bc1e-02f48c7fa73d';  -- INV-03361 2026-07-20 60 @ 7,166 (pcs->dus)
UPDATE invoice_items SET unit_index = 0 WHERE id = '7f62199c-0009-445e-8820-3d549d89aa38';  -- INV-03426 2026-07-23 5 @ 7,167 (pcs->dus)
UPDATE invoice_items SET unit_index = 0 WHERE id = 'ca56fef5-35e8-4b08-a4b2-ee88f15e8182';  -- INV-03535 2026-07-25 10 @ 7,167 (pcs->dus)
UPDATE invoice_items SET unit_index = 0 WHERE id = '1897baf4-abe0-43d2-848f-1dfbb2014042';  -- INV-03596 2026-07-26 20 @ 7,167 (pcs->dus)
-- Set Spork Hitam
UPDATE invoice_items SET unit_index = 0 WHERE id = 'd34c8724-f3bc-40b3-b36b-e0d4141038d8';  -- INV-02718 2026-07-09 20 @ 143,262 (pack->dus)
UPDATE invoice_items SET unit_index = 0 WHERE id = '4bc5fe7d-061d-4a62-9c64-acbc15ad767b';  -- INV-03574 2026-07-26 20 @ 167,000 (pack->dus)
-- PLASTIK ACPI KECIL
UPDATE invoice_items SET unit_index = 1 WHERE id = 'd8e3e5f9-6c75-45c7-899a-80f3b84b01c3';  -- INV-0004 2026-05-08 220 @ 3,800 (ikat->pack)
UPDATE invoice_items SET unit_index = 1 WHERE id = 'a24989ad-ccd9-4a28-9d7e-f59c59b289c9';  -- INV-0005 2026-05-08 30 @ 3,800 (ikat->pack)
-- PLASTIK ACPI BESAR
UPDATE invoice_items SET unit_index = 1 WHERE id = 'bc83795e-b29e-4739-82ba-47c165f92d60';  -- INV-0004 2026-05-08 220 @ 8,500 (ikat->pack)
UPDATE invoice_items SET unit_index = 1 WHERE id = '7b648c4b-ab04-4a75-a42f-16127267f2b4';  -- INV-0005 2026-05-08 15 @ 8,500 (ikat->pack)
-- PLASTIK ACPI SEDANG
UPDATE invoice_items SET unit_index = 1 WHERE id = '69ef3e5d-f3ae-4c7d-9604-51df3b28d63c';  -- INV-0004 2026-05-08 210 @ 4,900 (ikat->pack)
UPDATE invoice_items SET unit_index = 1 WHERE id = '0aeb2957-e0fc-4df3-916d-c062f4629df7';  -- INV-0005 2026-05-08 28 @ 4,900 (ikat->pack)
-- PLASTIK SAMPAH 60x100
UPDATE invoice_items SET unit_index = 1 WHERE id = 'bcf9a058-7508-440c-91c3-8ae5b51879e6';  -- INV-0004 2026-05-08 120 @ 12,000 (ikat->pack)
UPDATE invoice_items SET unit_index = 1 WHERE id = 'c3033ef1-5edd-465f-a2a9-034cb9043692';  -- INV-0005 2026-05-08 14 @ 12,000 (ikat->pack)
-- TISU KOTAK KECIL PULPIES
UPDATE invoice_items SET unit_index = 0 WHERE id = '6550c3e9-71c2-4e00-a36d-ccdf3f5f7f51';  -- INV-02454 2026-07-05 60 @ 151,000 (pack->dus)
UPDATE invoice_items SET unit_index = 0 WHERE id = '84778af6-89fc-4087-8bba-a16498b88ca8';  -- INV-02465 2026-07-05 45 @ 151,000 (pack->dus)
UPDATE invoice_items SET unit_index = 0 WHERE id = '700b42d3-c15a-4eaf-b9d7-648e413a729f';  -- INV-02943 2026-07-15 20 @ 151,000 (pack->dus)
UPDATE invoice_items SET unit_index = 0 WHERE id = '6a7bcf4d-c978-4d6c-b197-bb8b63039cfc';  -- INV-03284 2026-07-20 1 @ 151,000 (pack->dus)
UPDATE invoice_items SET unit_index = 0 WHERE id = '19b5db91-b1c5-4f80-b556-476febe5aed6';  -- INV-03343 2026-07-22 23 @ 151,000 (pack->dus)

-- ── 4. CABE RAWIT MERAH: cheap rows were entered per ons, not per kg ────────
--    (16 rows priced < 15.000 → ons(1); 8.000/ons = 80.000/kg, matching the kg rows)
UPDATE invoice_items SET unit_index = 1 WHERE id = '24fde3c4-77ba-474a-bedd-92681eeb8b21';  -- EXP-0077 2026-05-16 2.5 @ 7,600
UPDATE invoice_items SET unit_index = 1 WHERE id = 'd5add729-d04e-4c98-b594-70440d1ada33';  -- EXP-0118 2026-05-19 20 @ 7,500
UPDATE invoice_items SET unit_index = 1 WHERE id = 'ef36a556-9dce-4481-a031-8fd327f0670c';  -- EXP-0575 2026-05-28 10 @ 8,500
UPDATE invoice_items SET unit_index = 1 WHERE id = '7da4cf09-c157-407b-b389-422063f1300a';  -- EXP-0132 2026-05-21 10 @ 7,500
UPDATE invoice_items SET unit_index = 1 WHERE id = 'c42495f6-de9a-449e-a203-9ddacf1dead7';  -- EXP-0124 2026-05-20 10 @ 7,500
UPDATE invoice_items SET unit_index = 1 WHERE id = 'a9c55491-1b93-4736-a881-cbfb12fe6892';  -- EXP-0137 2026-05-21 20 @ 7,500
UPDATE invoice_items SET unit_index = 1 WHERE id = '53ea5520-3849-4480-b3cd-7b29c121543a';  -- EXP-0148 2026-05-22 10 @ 7,800
UPDATE invoice_items SET unit_index = 1 WHERE id = '888501f0-98da-4e1d-8f31-5c380dd66dfe';  -- EXP-0151 2026-05-22 10 @ 7,800
UPDATE invoice_items SET unit_index = 1 WHERE id = '0b65f528-4333-4fa1-b545-d0baebfe10cc';  -- EXP-0152 2026-05-22 10 @ 7,800
UPDATE invoice_items SET unit_index = 1 WHERE id = 'cef49869-46c5-4132-9355-8e98ac26a24e';  -- EXP-0557 2026-05-27 10 @ 8,000
UPDATE invoice_items SET unit_index = 1 WHERE id = '8913e047-144f-4e94-a945-e97e6cdaf8e2';  -- EXP-0288 2026-05-23 10 @ 8,000
UPDATE invoice_items SET unit_index = 1 WHERE id = '27522ada-be09-4b58-a141-d76ba3177e6f';  -- EXP-0294 2026-05-23 20 @ 8,000
UPDATE invoice_items SET unit_index = 1 WHERE id = 'bdf0817d-c559-4ad5-92c1-65dca7c2a2e9';  -- EXP-0368 2026-05-24 10 @ 8,000
UPDATE invoice_items SET unit_index = 1 WHERE id = 'cfadd150-db9e-4aec-92fa-6d0465d2f28d';  -- EXP-0376 2026-05-25 30 @ 8,000
UPDATE invoice_items SET unit_index = 1 WHERE id = '7b0dbfb7-b4f4-486f-92ac-ca4faaf3b699';  -- EXP-0470 2026-05-26 20 @ 8,000
UPDATE invoice_items SET unit_index = 1 WHERE id = '1860db9b-a185-4ab9-84c9-6542bee4c097';  -- EXP-0560 2026-05-27 20 @ 8,000

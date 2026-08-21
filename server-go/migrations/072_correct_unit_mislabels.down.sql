-- Reverse 072: restore original unit_index on every moved row, and drop the
-- two added units. Prices/quantities were never changed, so this is exact.

-- 1. invoice_items back to their pre-072 unit_index
UPDATE invoice_items SET unit_index = 1 WHERE id = '6769288c-b8f3-46ce-b421-4cd77006befb';
UPDATE invoice_items SET unit_index = 1 WHERE id = '3ee2f35b-0baf-4a4f-84d3-07e83a8db97b';
UPDATE invoice_items SET unit_index = 1 WHERE id = 'c71e788b-8746-41fb-8ed2-e4322da9f715';
UPDATE invoice_items SET unit_index = 1 WHERE id = '2246600f-8efe-4311-aa70-ffbe58ff5fec';
UPDATE invoice_items SET unit_index = 1 WHERE id = 'b74e3b14-84fd-4889-a074-061f7d9a1eaf';
UPDATE invoice_items SET unit_index = 1 WHERE id = '4c7a4ec8-efaa-4d61-be7f-4c1684ae3546';
UPDATE invoice_items SET unit_index = 1 WHERE id = 'b53cd983-44ec-4555-806b-1f61bca8d4b4';
UPDATE invoice_items SET unit_index = 1 WHERE id = '594843e9-69b2-4b8e-8130-d815c6e814ea';
UPDATE invoice_items SET unit_index = 1 WHERE id = '0ae37692-c154-4c04-8fb7-e3b38a3b3ea8';
UPDATE invoice_items SET unit_index = 1 WHERE id = 'f28035f0-779b-4d7c-aea5-5bbd9bbbb6ff';
UPDATE invoice_items SET unit_index = 1 WHERE id = '00dd292d-320e-4553-8b5a-235b6161d265';
UPDATE invoice_items SET unit_index = 1 WHERE id = 'fd419c18-d873-4152-abf4-fb662af951bb';
UPDATE invoice_items SET unit_index = 1 WHERE id = 'ce341712-b202-4350-b68e-5254d53050d7';
UPDATE invoice_items SET unit_index = 1 WHERE id = 'caa3b65c-5497-4858-b5c1-df686e15fa20';
UPDATE invoice_items SET unit_index = 1 WHERE id = 'ae94c53c-ba59-4d05-9773-3acf1ff89094';
UPDATE invoice_items SET unit_index = 1 WHERE id = '9351205d-da6c-41f6-9aa6-40f26ad4d42f';
UPDATE invoice_items SET unit_index = 1 WHERE id = '60bc7ec2-ac4b-4247-94a0-ceac27efb2b2';
UPDATE invoice_items SET unit_index = 1 WHERE id = '11dde5f5-e9c4-4b36-96ca-fc380582aed4';
UPDATE invoice_items SET unit_index = 1 WHERE id = 'a4f4a7b1-50c6-4334-8473-1c1c6ddd815b';
UPDATE invoice_items SET unit_index = 1 WHERE id = '26874a4d-b915-4ce3-a20d-2d3da9574aa4';
UPDATE invoice_items SET unit_index = 1 WHERE id = '64cdfad8-ea70-4c8d-ba40-f086e64089f0';
UPDATE invoice_items SET unit_index = 1 WHERE id = 'f068c5c3-de99-4ae3-a3cc-6e46cb8b31c5';
UPDATE invoice_items SET unit_index = 1 WHERE id = '6a00568a-f39e-4f68-848d-2050bb4ca0bb';
UPDATE invoice_items SET unit_index = 1 WHERE id = '184729f3-17d9-48d9-a4bd-76a71e3b9c94';
UPDATE invoice_items SET unit_index = 1 WHERE id = '1ec95edf-9f38-4111-abc0-cf7150a5b661';
UPDATE invoice_items SET unit_index = 0 WHERE id = 'bc83795e-b29e-4739-82ba-47c165f92d60';
UPDATE invoice_items SET unit_index = 0 WHERE id = '7b648c4b-ab04-4a75-a42f-16127267f2b4';
UPDATE invoice_items SET unit_index = 0 WHERE id = 'd8e3e5f9-6c75-45c7-899a-80f3b84b01c3';
UPDATE invoice_items SET unit_index = 0 WHERE id = 'a24989ad-ccd9-4a28-9d7e-f59c59b289c9';
UPDATE invoice_items SET unit_index = 0 WHERE id = '69ef3e5d-f3ae-4c7d-9604-51df3b28d63c';
UPDATE invoice_items SET unit_index = 0 WHERE id = '0aeb2957-e0fc-4df3-916d-c062f4629df7';
UPDATE invoice_items SET unit_index = 0 WHERE id = 'bcf9a058-7508-440c-91c3-8ae5b51879e6';
UPDATE invoice_items SET unit_index = 0 WHERE id = 'c3033ef1-5edd-465f-a2a9-034cb9043692';
UPDATE invoice_items SET unit_index = 1 WHERE id = 'd34c8724-f3bc-40b3-b36b-e0d4141038d8';
UPDATE invoice_items SET unit_index = 1 WHERE id = '4bc5fe7d-061d-4a62-9c64-acbc15ad767b';
UPDATE invoice_items SET unit_index = 1 WHERE id = '6550c3e9-71c2-4e00-a36d-ccdf3f5f7f51';
UPDATE invoice_items SET unit_index = 1 WHERE id = '84778af6-89fc-4087-8bba-a16498b88ca8';
UPDATE invoice_items SET unit_index = 1 WHERE id = '700b42d3-c15a-4eaf-b9d7-648e413a729f';
UPDATE invoice_items SET unit_index = 1 WHERE id = '6a7bcf4d-c978-4d6c-b197-bb8b63039cfc';
UPDATE invoice_items SET unit_index = 1 WHERE id = '19b5db91-b1c5-4f80-b556-476febe5aed6';
UPDATE invoice_items SET unit_index = 1 WHERE id = 'ad41f060-f9a0-4f74-a8f3-ca318614b48b';
UPDATE invoice_items SET unit_index = 1 WHERE id = '81404bd4-c71e-482a-b8af-b626b538848a';
UPDATE invoice_items SET unit_index = 1 WHERE id = 'e0414792-a28b-4a96-a6ee-08ae7070e033';
UPDATE invoice_items SET unit_index = 1 WHERE id = '8f6bb555-2d50-4d0b-8d64-04fd66e8fd31';
UPDATE invoice_items SET unit_index = 1 WHERE id = '99ac899a-c36f-4be8-aaa6-7a042c150a71';
UPDATE invoice_items SET unit_index = 1 WHERE id = 'ce7c2103-ca1f-421e-b58d-aac13be37097';
UPDATE invoice_items SET unit_index = 1 WHERE id = 'b9555192-75db-4833-9435-a2756412f7af';
UPDATE invoice_items SET unit_index = 1 WHERE id = '0dce3a99-3a2b-496a-8755-940c85d38d98';
UPDATE invoice_items SET unit_index = 1 WHERE id = '7738a1af-2b59-4188-85ce-ced62a46044b';
UPDATE invoice_items SET unit_index = 1 WHERE id = '6c54b834-e85e-4c17-a77e-24e62a2c9744';
UPDATE invoice_items SET unit_index = 1 WHERE id = '8a978e60-b830-452c-b181-30e6893afa6e';
UPDATE invoice_items SET unit_index = 1 WHERE id = '3f97b4c4-8d23-4c2b-b060-b17bdfbdb73e';
UPDATE invoice_items SET unit_index = 1 WHERE id = 'c4e6c185-d21a-4603-acd3-f6d06d4e3582';
UPDATE invoice_items SET unit_index = 1 WHERE id = 'e6e05667-242b-4549-b0e8-16f661024d75';
UPDATE invoice_items SET unit_index = 1 WHERE id = '71f33098-9041-42c7-9a43-8772ad2fd22e';
UPDATE invoice_items SET unit_index = 1 WHERE id = 'e46a3721-bc02-4d32-bc1e-02f48c7fa73d';
UPDATE invoice_items SET unit_index = 1 WHERE id = '7f62199c-0009-445e-8820-3d549d89aa38';
UPDATE invoice_items SET unit_index = 1 WHERE id = 'ca56fef5-35e8-4b08-a4b2-ee88f15e8182';
UPDATE invoice_items SET unit_index = 1 WHERE id = '1897baf4-abe0-43d2-848f-1dfbb2014042';
UPDATE invoice_items SET unit_index = 0 WHERE id = '24fde3c4-77ba-474a-bedd-92681eeb8b21';
UPDATE invoice_items SET unit_index = 0 WHERE id = 'd5add729-d04e-4c98-b594-70440d1ada33';
UPDATE invoice_items SET unit_index = 0 WHERE id = 'ef36a556-9dce-4481-a031-8fd327f0670c';
UPDATE invoice_items SET unit_index = 0 WHERE id = '7da4cf09-c157-407b-b389-422063f1300a';
UPDATE invoice_items SET unit_index = 0 WHERE id = 'c42495f6-de9a-449e-a203-9ddacf1dead7';
UPDATE invoice_items SET unit_index = 0 WHERE id = 'a9c55491-1b93-4736-a881-cbfb12fe6892';
UPDATE invoice_items SET unit_index = 0 WHERE id = '53ea5520-3849-4480-b3cd-7b29c121543a';
UPDATE invoice_items SET unit_index = 0 WHERE id = '888501f0-98da-4e1d-8f31-5c380dd66dfe';
UPDATE invoice_items SET unit_index = 0 WHERE id = '0b65f528-4333-4fa1-b545-d0baebfe10cc';
UPDATE invoice_items SET unit_index = 0 WHERE id = 'cef49869-46c5-4132-9355-8e98ac26a24e';
UPDATE invoice_items SET unit_index = 0 WHERE id = '8913e047-144f-4e94-a945-e97e6cdaf8e2';
UPDATE invoice_items SET unit_index = 0 WHERE id = '27522ada-be09-4b58-a141-d76ba3177e6f';
UPDATE invoice_items SET unit_index = 0 WHERE id = 'bdf0817d-c559-4ad5-92c1-65dca7c2a2e9';
UPDATE invoice_items SET unit_index = 0 WHERE id = 'cfadd150-db9e-4aec-92fa-6d0465d2f28d';
UPDATE invoice_items SET unit_index = 0 WHERE id = '7b0dbfb7-b4f4-486f-92ac-ca4faaf3b699';
UPDATE invoice_items SET unit_index = 0 WHERE id = '1860db9b-a185-4ab9-84c9-6542bee4c097';

-- 2. unit definitions back to single-unit
UPDATE items SET units = '[{"name": "ball", "perPrev": null}]'::jsonb WHERE id = 'ab18b254-cde8-4072-a733-2ead03d22089';  -- TERIGU CAKRA
UPDATE items SET units = '[{"name": "kg", "perPrev": null}]'::jsonb WHERE id = 'fcc611ff-932b-49c8-a3f3-34d4b86ca023';  -- CABE RAWIT MERAH

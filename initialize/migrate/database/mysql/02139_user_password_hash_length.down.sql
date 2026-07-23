ALTER TABLE `user`
    MODIFY COLUMN `password` VARCHAR(100) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL COMMENT 'User Password';

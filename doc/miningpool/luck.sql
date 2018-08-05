CREATE TABLE `luck` (
  `id` int(11) NOT NULL AUTO_INCREMENT,
  `height` int(11) DEFAULT NULL,
  `coinid` int(11) DEFAULT NULL,
  `diff` bigint(30) DEFAULT NULL,
  `sumdiff` bigint(30) DEFAULT NULL,
  `blockdiff` bigint(30) DEFAULT NULL,
  `luck` double DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `id_UNIQUE` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8;
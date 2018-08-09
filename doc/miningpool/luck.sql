CREATE TABLE `luck` (
  `id` int(11) NOT NULL AUTO_INCREMENT,
  `height` int(11) DEFAULT NULL,
  `coinid` int(11) DEFAULT NULL,
  `diff` double DEFAULT NULL,
  `sumdiff` double DEFAULT NULL,
  `blockdiff` double DEFAULT NULL,
  `luck` double DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `id_UNIQUE` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8
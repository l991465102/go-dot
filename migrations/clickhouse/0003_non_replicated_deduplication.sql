ALTER TABLE tracking_event MODIFY SETTING non_replicated_deduplication_window = 1000;

ALTER TABLE tracking_invalid_event MODIFY SETTING non_replicated_deduplication_window = 1000;

ALTER TABLE matches
  ADD COLUMN IF NOT EXISTS automation JSONB NOT NULL DEFAULT '{"mode":"active","eventTypes":["kickoff","goal","shot","big_chance","save","miss","foul","yellow_card","red_card","var_check","penalty","substitution","injury","tactical_shift","pressure","halftime","fulltime"],"cooldownSeconds":8}'::jsonb;

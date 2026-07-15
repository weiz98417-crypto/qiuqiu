ALTER TABLE matches
  ALTER COLUMN automation SET DEFAULT '{"mode":"active","eventTypes":["kickoff","goal","shot","big_chance","save","miss","foul","yellow_card","red_card","var_check","var_result","goal_cancelled","penalty","penalty_awarded","substitution","injury","tactical_shift","pressure","halftime","fulltime","match_end"],"cooldownSeconds":90}'::jsonb;

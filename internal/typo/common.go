package typo

import "strings"

// common are real English words that are one or two edits from a rule's
// word, and common enough that someone meant them: "whether" isn't
// "weather", "test" isn't "tests", "plain" isn't "plan". Rare words, such as
// "wether" (a sheep), aren't here, so they're still corrected: the person
// almost always meant the rule's word.
//
// They were picked from the dictionary words near the rules' vocabulary.
// When a rule gains a word, check its near neighbours the same way.
var common = toSet(`
abort action again alight also always among angle attacker attainment bake ball band bash bead bear
beast beat beet bell belt bend bent best better bind bite bland blast bleed blight blinding blinks
blower bold book both bout braid bran branched brandy bread bred bright brunch bull bullying bush
bust cage cake came card care cart cash cast cave cell cent change changed changer chat cheek chick
chose chuck clan coast coat code cold colder colt come comet commander commando commend companion
compassion cone cones construct contact contractor contradict contrary contrast cook cool cord core
cosy counteract crab crash crate creed cull curd currency currently dale dame damn dare dark darn
dash data dead deadline deal dear deceased december decent deep defection deflection delegate
deletion dell dent deplete deport derail desk detached detachment detain detection dialysis dinner
direction directly director disk document dogs dome done door dose dote dour down drab drawn dread
duck dull dunk dusky dust each east eight ejection elective electron elevation else even ever every
execution exertion extracted extractor face fact fail fainting fake fast fate fawn fear feather feel
fell fend fiend fight filer fill film fine fire fist five fives flat flight fodder folded follower
fond food fool foot forecaster foremost forge forged forger forgot fork form former forth four free
freed fright frock front fuel full fund gain gale game gate gave gear geek getter gift gist give
goes gold golder good grade gram grand grape grate gray greed grub gull gush gust half halve hand
hard hare hash hate have haven haze head headline headliner heap hear heard heather heave heck held
hell helm hemp here hike hill hint hire hold holder home hook hope hose host hour hull hush husk
idea identity infect inject inks inner inscribe insert inset inspect irate item jeep jest join joke
jump keel keen kelp kept kill kind king kink knew knight knob knot known lace lake land lapel lard
lash lass late latent latter lawn lead leaf lease least leather leek left less libel lift light like
line link lint lisp list litter lock long loom loop loot lose loss lost lots loud love lower lust
made make maker male mane many mare mark mash mast matching mate maze meal mean meek meet mend mere
might mike mild mile miles mill mine mink mint miss mist mock mode molder monument moon more most
motel mould move muck mulch munch mush musk must name nation near neat need nest never newt next
nice nigh nights node none nook nope nose noted null odds olden older omen once only outlined
outlive oven over owen oxen pace pack page paid palace pale pane pant panting park part pass past
path pave pawn peace peak pear peek peep pest pick piles pill pine place plain plan plane planet
plank plant plate play pleasure plight plot plush pointing poll pool poor port pose posh post pour
prescribe presume preview priced prick pride prime prince print printing prize prod produce produced
producer projector prone proof prop props prose prospect protect pulp pure purl quilting race rack
rage raid rain rake ranch rank rant rare rated ration rave reach ready real reap rear recant redo
reed reflection regent rehearse rejection relegate relent rely rend repeated repent reporter resent
resort rest result retail retailer retort revealed reversed rice ride ring rise rite road rock rocky
role roll room root rope rose rote rush rust safe sage sail sake salt same sane saved saver says
scab scale scam scar score scored scorer seal sear searcher seat seed seek seem seen seep selection
self sell send sent setter shake shave shed shock shoe shook shop short shot shoulder shout show
shown shut side sides sigh sight sign sing sink site size slant slap slave slight slip slop slot
slow snag snare snip snort snow soap sock soft sold sole song sons soon sore sort soul sour span
sport spot stack stake stale star starch start statue stay step stick still stocks stocky stood
stool stoop store stork story stow strategic stuck subset such suit sundries sure surprise swan swap
take taken taker tale talk tall tame tape task tear teem tell tend tent term terminate test text
than that's thaw thee their theme then there these they thin thing think those three thus tide tight
tile till time tiny toil told toll tone took tool tour town trace trader trash tread tree trek trim
trip trite true tune turn tusk type typo unannounced unattached unit unreleased unset until upon
used user vast vent very vest view vile visit vote wage wait wake walk wall want ward warm wash
washroom wave weak wear weathered weed week weep well went were west wheat when whet whether which
white whole whose wide wife wild will wind winder wine winter wise wish with wood wool word work
world worse worth would wrap writer wrote yard yawn yeah year yell yelp your yours zest
`)

func toSet(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.Fields(s) {
		out[w] = true
	}
	return out
}

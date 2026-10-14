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
abort action again also always among angle bake band bear beast beat beet bell belt bend bent best
bind bite bland blast blinks bold book both bout braid bran branched brandy bread bright brunch
bull bush bust cake came card care cast cave cell cent change changed changer chat cheek chick
chose chuck clan coast coat code cold colder colt come comet commend commander commando companion
compassion cone cones cook cool cord core cosy crate cull currency currently dale dame dare dark
data dead deadline deal dear deceased december deep defection deflection delegate deletion dell
dent deplete deport derail detached detain detection dialysis dinner direction directly director
document dogs dome done door dose dote dread duck dull dust each east eight ejection elective
electron elevation else even ever every execution exertion extracted extractor face fact fail
fake fast fate fear feather feel fell fend fight fill film fine fire fist five flat fodder folded
fond food fool foot form former forecaster foremost forge forged forger forgot fork forth four
free fright full fund gain gale game gate gave gear geek gift gist give goes gold golder good
grade grand grate gull gush gust half halve hand hard hare hate have haven haze head headline
headliner heap hear heard heather heave heck held hell helm hemp here hike hint hire hold holder
home hook hope host hour hull hush idea identity inks inner irate item jeep jest join joke jump
keel keen kelp kept kind knew knight knob knot known lake land lash lass late latent lead leaf
lease least leather leek left less lift light like line link lint lisp list lock long loom loop
loot lose loss lost lots loud love made maker male mane many mare mark mast matching mate maze
meal mean meek meet mend mere might mike mild mile miles mine mink mint miss mist mode molder
moon more most motel mould move muck mulch munch mush must name nation near neat need nest never
newt next nice nigh nights node none nope nose noted null odds older omen once only outlined
outlive oven over owen oxen pace pack page paid pale park part pass past path pave peak pear
peek peep pest pick pine place plain plane plank plant play pleasure plush poll pool poor port
pose posh post pour preview priced prick pride prime prince print prize prod produce produced
producer projector prone proof prop props prose prospect protect pulp pure purl race rage raid
rain rake ranch rank rant rare rated ration rave reach ready real reap rear redo reed reflection
rehearse rejection relegate rely repeated reporter resort rest result retail retailer retort
revealed reversed rice ride ring rise rite road rock role roll room root rope rose rote rush rust
safe sage sail sake salt same sane saved saver says scale score scored scorer seal sear searcher
seat seed seek seem seen seep selection self sell send sent shave shed shock shoe shop shot
shoulder shown side sight sign sing sink site size slave slip slop slow snow soap sock soft sold
sole song sons soon sore sort soul sour spot stack stale star starch start statue stay step
stick still stocks stocky stood stool stoop store stork story stow strategic stuck such suit
sure take tale talk tall tape task tear teem tell tend tent term terminate test text than that's
thaw thee their theme then there these they thin thing those thus tide tight tile till time
tiny toil told toll tone took tool tour town trace trader tread tree trim trip trite true tune
turn type unannounced unit unreleased until upon used user vast vent very vest view vile visit
vote wait wake walk wall want warm wash wave weak wear weathered weed week weep well went were
west wheat whet when whether which white whole wide wife wild will wind winder wine winter wise
wish with wood wool word work world worse worth would wrap writer wrote yard yeah year yell yelp
your yours zest
`)

func toSet(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.Fields(s) {
		out[w] = true
	}
	return out
}

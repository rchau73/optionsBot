// Server-side only: about 1,000 of the most common passwords (#680), lower case,
// sorted. Source: the words at the top of Mark Burnett's public-domain list of
// the 10,000 most common passwords, typed in rather than downloaded, so it
// follows that list without copying it line by line; a few long entries
// (at least 12 characters, the shortest password allowed) were added because
// the list's short ones only match through passwordIsCommon's stripping.
// Public data, not personal data: the bundle is the only copy, no call goes out.
const WORDS = `
0000 000000 000000000000 01012000 0123456789 01234567890 098765 0987654321 09876543210 101010 10101010 1111 11111 111111
1111111 11111111 111111111 1111111111 111111111111 11112222 111222 111222333 112233 11223344 1122334455 112233445566
1212 121212 12121212 121212121212 123123 123123123 123123123123 123321 1234 12341234 12344321 12345 1234512345
123451234512345 123456 1234567 12345678 123456789 1234567890 123456789012 1234567890123 12345678901234 123456789012345
1234567890123456 1234567890q 1234567891 12345678910 123456789123 123456789a 12345678a 123456a 123456q 123456qwerty
12345a 12345qwert 12345qwerty 1234abcd 1234asdf 1234qwer 1234qwerasdf 1234qwerty 123654 123abc 123qwe 131313 13131313
147258 147258369 147852 147852369 159357 159753 159951 1969 1970 1971 1972 1973 1974 1975 1976 1977 1978 1979 1980 1981
1982 1983 1984 1985 1986 1987 1988 1989 1990 1991 1992 1993 1994 1995 1q2w3e 1q2w3e4r 1q2w3e4r5t 1q2w3e4r5t6y
1q2w3e4r5t6y7u 1q2w3e4r5t6y7u8i 1qaz1qaz 1qaz2wsx 1qaz2wsx3edc 1qaz2wsx3edc4rfv 1qazxsw2 2000 20202020 2222 22222 222222
22222222 222222222222 232323 258369 258456 2wsx3edc 321321 321321321 3333 333333 33333333 333333333333 4444 444444
44444444 444444444444 456123 456456 456789 5555 55555 555555 55555555 555555555555 654321 666666 66666666 666666666666
6969 696969 741852 741852963 7777 777777 7777777 77777777 777777777777 789123 789456 789456123 789789 852456 8675309
87654321 8888 888888 88888888 888888888888 963852 963852741 987654 987654321 9876543210 987654321012 987654321098 9999
99999 999999 99999999 999999999999 a1b2c3 a1b2c3d4 a1b2c3d4e5 a1b2c3d4e5f6 aa123456 aa12345678 aaa111 aaaa aaaaa aaaaaa
aaaaaaa aaaaaaaa abc123 abc1234 abc12345 abc123456 abcabc abcabcabc abcd abcde abcdef abcdefg abcdefgh abcdefghi
abcdefghijkl abcdefghijklm abcdefghijklmn abcdefghijklmnop abcdefghijklmnopqrstuvwxyz access adidas admin adminadmin
administrador administrator administrator1 airforce airplane albert alexander alien amanda amazon america andrea andrew
android angel angela angels anthony apple apples april argentina army arsenal asd123 asdasd asdasdasd asdf asdfasdf
asdfg asdfgh asdfghjk asdfghjkl ashley assassin asshole audi august austin australia autumn azerty azertyuiop baby
babyboy babygirl babylon5 bacon badboy bailey banana bandit barcelona barney bart baseball basketball batman bear bears
beautiful beauty beckham beer beijing berlin bestfriend bigdaddy bigdog bigtits bigtruck billionaire binance bitch
bitcoin biteme black blank blessed blizzard blockchain blowjob blowme blue booboo boobs booger boomer boston botafogo
brandon brandy brasil brazil britain broncos brother brown bubbles bulldog bulls bullshit bunny burger business buster
butter butterfly butthead c3po california calvin camaro cameron canada candy captain captainamerica carlos cartman
casper catdog cats celtics changeme changethis charles charlie cheese chelsea cherry chester chevy chewbacca chicago
chicken china chocolate chris christ cobra cocacola coffee coinbase college company compaq computer cookie cookies
cooper corinthians correcthorsebatterystaple corvette counter cowboy cowboys crow cruzeiro crypto crystal cunt cupcake
cutie cutiepie daddy daisy dakota dallas dance dancer dancing daniel danielle darling darthvader database daughter david
deadpool debian december default defaultpassword dell demo demon dennis denver deribit devil dexter diablo diamond dick
discord docker doctor doctorwho dodge doggie doggy dogs dolphin dolphins dragon dragonball dragons drummer ducati eagle
eagles edward elephant elizabeth emerald empty england enter ethereum everton example facebook faith falcon fall family
familyguy father february fender ferrari firefighter fireman fishing flamengo florida flower flowers fluminense football
ford forever fortnite france freedom friday friends frodo frog fuck fucker fuckme fuckoff fuckyou futurama gandalf
gateway gemini george germany gfhjkm ghbdtn ghost ghosts giants ginger giraffe gmail goku gold golden golf golfer
golfing gollum goodbye goodluck goodmorning goodnight google gorgeous gorilla grandma grandpa gray green gremio grey
gryffindor guest guitar gunner hammer handsome hannah happiness happy happyday hardcore harley harrypotter hawk heather
heaven hell hello hellokitty hellothere helloworld helloworld123 helpme hermione hockey hodl homer honda honey horny
horse horses hotdog hotmail hottie houston hulk hunter husband icecream iceman ilovegod iloveme ilovemusic ilovemyfamily
iloveu iloveyou iloveyouforever iloveyouiloveyou iloveyousomuch india instagram intel internet iphone ireland ironman
italy iwantu jackie jackson jade jaguar james january japan jasmine jasper jeep jennifer jessica jesus john johnny
johnson joker jonathan jordan joseph joshua july june junior justin juventus kawasaki kenny kevin killer king kingkong
kitten kitty klaster knicks knight koala kobe korea kubernetes ladybug lakers lamborghini lasvegas lauren lebron legolas
lemon lenovo letmein letmeinletmein letmeinnow lily lime linkedin linux lion lions lisa liverpool lizard london love
lovely loveme lovers lucifer lucky luckyme maddog madison maggie magic magician mama manager manchester manchesterunited
mango manutd maradona march maria123 marina marine marines mario marlboro martin master masterkey mastermind matrix
matthew maverick melbourne melissa mercedes merlin messi metamask mexico miami michael michelle mickey microsoft
midnight mike miller millionaire minecraft mommy monday money moneymoney mongodb monkey monster moonlight morgan
morpheus moscow mother motorcycle muffin murphy music musician mustang mybaby myfamily mypass mypassword mysecret mysql
nakamoto naruto nascar natasha nathan navy ncc1701 netflix network newpassword newyork neymar nicholas nicole nike
nikita niners ninja nintendo nissan nokia none nopass nopassword nothing november null nurse october office oldpassword
oliver onepiece onetwothree onetwothreefour onyx open opensesame openup options oracle orange orchid oscar ou812
p4ssw0rd pa55word packers pacman palmeiras panda pantera panther panties papa paris pass passpass passw0rd password
password1234 password12345 password123456 password2024 password2025 password2026 passwordpassword passwords patrick
patriots peach peaches peanut pearl pele penguin penis pepper pepsi phoenix pilot pineapple pink pinkie pinky pizza
platinum player playstation please pokemon police pony pookie porsche portugal postgres postgresql predator pretty
prince princesa princess private puma pumpkin puppies puppy purple pussy pussycat python q1w2e3 q1w2e3r4 q1w2e3r4t5
q1w2e3r4t5y6 q1w2e3r4t5y6u7 q2w3e4r5 qazwsx qazwsxedc qazwsxedc123 qazwsxedcrfv qazwsxedcrfvtgb qazxsw queen qwaszx
qwe123 qweasd qweasdzxc qweasdzxc123 qweqwe qweqweqwe qwer1234 qwert qwerty qwerty123456 qwertyui qwertyuiop
qwertyuiop123 qwertyuiop1234 qwertyuiop12345 qwertyuiopasdfghjkl qwertyuiopasdfghjklzxcvbnm qwertz qwertzuiop r2d2
rabbit rachel raiders rainbow rambo ranger rangers raven ravens razz realmadrid redis redskins redsox redwings reebok
rich1234 richard riodejaneiro robert roblox robocop robot rock rocket rockon rockstar rocky rockyou roma rome ronaldo
root rootroot rose rosebud roses ruby runescape runner running russia samantha sample samsung samurai sandra santos
saopaulo sapphire sasuke satan satoshi saturday school scooby scooter scorpion scotland scott seattle sebastian secret
secure security september server service sesame sexsex sexy shadow shannon shark sharks sherlock shit shithead sierra
silver simpsons singer sister skateboard skating skywalker slayer slipknot smile smiley smokey snake sniper snoopy
snowboard soccer soldier sonic sony sophie southpark spain spanky sparky spider spiderman spotify spring star starcraft
stargate starlight stars startrek starwars steelers stella steven strawberry student stupid subaru sugar summer sunday
sunflower sunshine superman supersecret superstar support surfing suzuki sweet sweetheart sweetie sweety swimming sword
sydney system tardis taylor teacher telegram temp temporary tennis tequila terminator tesla test tester testing testtest
texas theman thepassword thequickbrownfox thequickbrownfoxjumpsoverthelazydog thomas thor thunder thursday thx1138
tiffany tiger tigers tigger tiktok titties tokyo toor topsecret toronto tottenham toyota trader trading trinity trouble
truck trustme trustno1 trustno1trustno1 tucker tuesday tulip turtle twitter ubuntu unitedstates university unknown
usa123 user username vagina vampire vancouver vanilla vasco vegas vegeta victor victoria viking viper vodka volkswagen
volvo wallet warcraft warrior wednesday welcome welcomeback whale whatever whatsapp whiskey white whoami wife william
wilson windows wine winner winston winter witch wizard wolverine xbox xxxxxx yahoo yamaha yankees yellow yoda
yourpassword youtube zaq1 zaq12wsx zaq1xsw2 zaq1zaq1 zebra zelda zion zombie zxc123 zxcv zxcvb zxcvbn zxcvbnm zxczxc
`;

/** Every entry, for the lookup in password.js. */
export const COMMON_PASSWORDS = new Set(WORDS.split(/\s+/).filter(Boolean));

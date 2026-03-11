# PLAN DRAFT

IMPROVEMENT ENHANCMENT THIS PROJECT.

requirement:

- ability to see all quiz in the future or current
- ability to see all assignment in the future or current
- save all to sqlite, purpose to save to flag already reminder if not finish
  - save
  - update flag reminder, reminder first found quiz or assignment send to whatsapp notif service (already implemented to other service, should be localhost:5000 fti-wa-bot), send if quiz will be close 24 hours and 12 hours.
  - submitted flag
- quiz id and assign id should be unique by design, but if necessary to own uuid on this system.
- quiz or assignment can be manual or technical or just easy one, the idea system can suggest the answer or guide to tackle the task quiz or assignments then also send if sugggestion is ready by system.

goal: user can be notify if any quiz or assignment in the future and finish the quiz or assignments.

current feature ready:

- login
- get courses
- get attendance
- submit attendance

material sample:
course url: https://elearning.budiluhur.ac.id/course/view.php?id=31389 (31389 is course id)
assignment list of course url: https://elearning.budiluhur.ac.id/mod/assign/index.php?id=31389 (course id)
quiz list of couse url: https://elearning.budiluhur.ac.id/mod/quiz/index.php?id=31160
quiz detail url: https://elearning.budiluhur.ac.id/mod/quiz/view.php?id=1482510
sample HTML of assignment/quiz list of course is:

```

<div role="main"><span id="maincontent"></span><h2>Assignments</h2><div class="theme-table-wrap"><table class="generaltable table table-striped">
<thead>
<tr>
<th class="header c0" style="text-align:left;" scope="col">Topic</th>
<th class="header c1" style="text-align:left;" scope="col">Assignments</th>
<th class="header c2" style="text-align:center;" scope="col">Due date</th>
<th class="header c3" style="text-align:right;" scope="col">Submission</th>
<th class="header c4 lastcol" style="text-align:right;" scope="col">Grade</th>
</tr>
</thead>
<tbody><tr class="">
<td class="cell c0" style="text-align:left;">02. Dasar-Dasar Pemrograman GUI, Merancang Database, Testing [21 Februari 2026]</td>
<td class="cell c1" style="text-align:left;"><a href="https://elearning.budiluhur.ac.id/mod/assign/view.php?id=1488564">Quiz-2</a></td>
<td class="cell c2" style="text-align:center;">Saturday, 28 February 2026, 11:59 PM</td>
<td class="cell c3" style="text-align:right;">Submitted for grading</td>
<td class="cell c4 lastcol" style="text-align:right;">-</td>
</tr>
<tr><td colspan="5"><div class="tabledivider"></div></td></tr>
<tr class="">
<td class="cell c0" style="text-align:left;">03. Master Pelanggan [27 Februari 2026]</td>
<td class="cell c1" style="text-align:left;"><a href="https://elearning.budiluhur.ac.id/mod/assign/view.php?id=1490221">Quiz-03</a></td>
<td class="cell c2" style="text-align:center;">Saturday, 7 March 2026, 11:59 PM</td>
<td class="cell c3" style="text-align:right;">Submitted for grading</td>
<td class="cell c4 lastcol" style="text-align:right;">-</td>
</tr>
<tr><td colspan="5"><div class="tabledivider"></div></td></tr>
<tr class="">
<td class="cell c0" style="text-align:left;">4. Master Kategori Barang[28 Februari 2026]</td>
<td class="cell c1" style="text-align:left;"><a href="https://elearning.budiluhur.ac.id/mod/assign/view.php?id=1509849">Quiz-04</a></td>
<td class="cell c2" style="text-align:center;">Friday, 13 March 2026, 11:59 PM</td>
<td class="cell c3" style="text-align:right;">No submission</td>
<td class="cell c4 lastcol" style="text-align:right;">-</td>
</tr>
<tr><td colspan="5"><div class="tabledivider"></div></td></tr>
<tr class="lastrow">
<td class="cell c0" style="text-align:left;">5. Master Petugas[06 Maret 2026]</td>
<td class="cell c1" style="text-align:left;"><a href="https://elearning.budiluhur.ac.id/mod/assign/view.php?id=1509863">Quiz-05</a></td>
<td class="cell c2" style="text-align:center;">Friday, 13 March 2026, 12:00 AM</td>
<td class="cell c3" style="text-align:right;">No submission</td>
<td class="cell c4 lastcol" style="text-align:right;">-</td>
</tr>
</tbody>
</table></div>
</div>
```

Submitted mean: Im already submit.

example of detail quiz or assignments:
https://elearning.budiluhur.ac.id/mod/assign/view.php?id=1488564 (on list table above assignments or quiz) there is id detail quiz or assignment

sample detail QUIZ:

```
<div role="main"><span id="maincontent"></span><h2>Quiz-2</h2><div id="intro" class="box py-3 generalbox boxaligncenter"><div class="no-overflow"><p>Silakan Kumpulkan Database</p></div></div><div class="submissionstatustable"><h3>Submission status</h3><div class="box py-3 boxaligncenter submissionsummarytable"><div class="theme-table-wrap"><table class="generaltable table table-striped">
<tbody><tr class="">
<th class="cell c0" style="" scope="row">Submission status</th>
<td class="submissionstatussubmitted cell c1 lastcol" style="">Submitted for grading</td>
</tr>
<tr class="">
<th class="cell c0" style="" scope="row">Grading status</th>
<td class="submissionnotgraded cell c1 lastcol" style="">Not graded</td>
</tr>
<tr class="">
<th class="cell c0" style="" scope="row">Due date</th>
<td class="cell c1 lastcol" style="">Saturday, 28 February 2026, 11:59 PM</td>
</tr>
<tr class="">
<th class="cell c0" style="" scope="row">Time remaining</th>
<td class="earlysubmission cell c1 lastcol" style="">Assignment was submitted 1 hour 34 mins early</td>
</tr>
<tr class="">
<th class="cell c0" style="" scope="row">Last modified</th>
<td class="cell c1 lastcol" style="">Saturday, 28 February 2026, 10:24 PM</td>
</tr>
<tr class="">
<th class="cell c0" style="" scope="row">File submissions</th>
<td class="cell c1 lastcol" style=""><div class="box py-3 boxaligncenter plugincontentsummary summary_assignsubmission_file_2460466"><div id="assign_files_tree69b1aa7fd3c5e34"><div class="ygtvitem" id="ygtv0"><div class="ygtvchildren" id="ygtvc0"><div class="ygtvitem" id="ygtv1"><table id="ygtvtableel1" border="0" cellpadding="0" cellspacing="0" class="ygtvtable ygtvdepth0 ygtv-expanded ygtv-highlight0"><tbody><tr class="ygtvrow"><td id="ygtvt1" class="ygtvcell ygtvtn"><a href="#" class="ygtvspacer">&nbsp;</a></td><td id="ygtvcontentel1" class="ygtvcell ygtvhtml ygtvcontent"><div><div class="fileuploadsubmission"><img class="icon icon" alt="DB_Penjualan_full.sql" title="DB_Penjualan_full.sql" src="https://elearning.budiluhur.ac.id/theme/image.php/mb2cg/core/1728526650/f/text"> <a target="_blank" href="https://elearning.budiluhur.ac.id/pluginfile.php/2818744/assignsubmission_file/submission_files/2460466/DB_Penjualan_full.sql?forcedownload=1">DB_Penjualan_full.sql</a>   </div><div class="fileuploadsubmissiontime">28 February 2026, 10:24 PM</div></div></td></tr></tbody></table><div class="ygtvchildren" id="ygtvc1" style="display:none;"></div></div><div class="ygtvitem" id="ygtv2"><table id="ygtvtableel2" border="0" cellpadding="0" cellspacing="0" class="ygtvtable ygtvdepth0 ygtv-expanded ygtv-highlight0"><tbody><tr class="ygtvrow"><td id="ygtvt2" class="ygtvcell ygtvln"><a href="#" class="ygtvspacer">&nbsp;</a></td><td id="ygtvcontentel2" class="ygtvcell ygtvhtml ygtvcontent"><div><div class="fileuploadsubmission"><img class="icon icon" alt="DB_Penjualan_structure.sql" title="DB_Penjualan_structure.sql" src="https://elearning.budiluhur.ac.id/theme/image.php/mb2cg/core/1728526650/f/text"> <a target="_blank" href="https://elearning.budiluhur.ac.id/pluginfile.php/2818744/assignsubmission_file/submission_files/2460466/DB_Penjualan_structure.sql?forcedownload=1">DB_Penjualan_structure.sql</a>   </div><div class="fileuploadsubmissiontime">28 February 2026, 10:24 PM</div></div></td></tr></tbody></table><div class="ygtvchildren" id="ygtvc2" style="display:none;"></div></div></div></div></div></div></td>
</tr>
<tr class="lastrow">
<th class="cell c0" style="" scope="row">Submission comments</th>
<td class="cell c1 lastcol" style=""><div class="box py-3 boxaligncenter plugincontentsummary summary_assignsubmission_comments_2460466"><div class="commentscontainer"><div style="display:none" id="cmt-tmpl"><div class="comment-message"><div class="comment-message-meta mr-3"><span class="picture">___picture___</span><span class="user">___name___</span> - <span class="time">___time___</span></div><div class="text">___content___</div></div></div><div class="mdl-left"><a class="showcommentsnonjs" href="https://elearning.budiluhur.ac.id/mod/assign/view.php?id=1488564&amp;rownum=0&amp;useridlistid=69b1aa7fcee67225079024&amp;action&amp;nonjscomment=1&amp;comment_itemid=2460466&amp;comment_context=2818744&amp;comment_component=assignsubmission_comments&amp;comment_area=submission_comments">Show comments</a><a class="comment-link" id="comment-link-69b1aa7fe5cdf" href="#" role="button" aria-expanded="false"><i class="icon fa fa-caret-right fa-fw " title="Comments" aria-label="Comments"></i><span id="comment-link-text-69b1aa7fe5cdf">Comments (0)</span></a><div id="comment-ctrl-69b1aa7fe5cdf" class="comment-ctrl"><ul id="comment-list-69b1aa7fe5cdf" class="comment-list"><li class="first"></li></ul><div id="comment-pagination-69b1aa7fe5cdf" class="comment-pagination"></div><div class="comment-area"><div class="db"><textarea name="content" rows="2" id="dlg-content-69b1aa7fe5cdf" aria-label="Add a comment..." cols="20" style="color: grey;"></textarea></div><div class="fd" id="comment-action-69b1aa7fe5cdf"><a id="comment-action-post-69b1aa7fe5cdf" href="#">Save comment</a><span> | </span><a id="comment-action-cancel-69b1aa7fe5cdf" href="#">Cancel</a></div></div><div class="clearer"></div></div></div></div></div></td>
</tr>
</tbody>
</table></div>
</div><div class="box py-3 generalbox submissionaction"><div class="singlebutton">
    <form method="get" action="https://elearning.budiluhur.ac.id/mod/assign/view.php">
            <input type="hidden" name="id" value="1488564">
            <input type="hidden" name="action" value="editsubmission">
        <button type="submit" class="btn btn-secondary" id="single_button69b1aa7fd3c5e37" title="">Edit submission</button>
    </form>
</div><div class="singlebutton">
    <form method="get" action="https://elearning.budiluhur.ac.id/mod/assign/view.php">
            <input type="hidden" name="id" value="1488564">
            <input type="hidden" name="action" value="removesubmissionconfirm">
        <button type="submit" class="btn btn-secondary" id="single_button69b1aa7fd3c5e38" title="">Remove submission</button>
    </form>
</div><div class="box py-3 boxaligncenter submithelp">You can still make changes to your submission.</div></div></div></div>
```

sample of detail quiz:

````<div role="main"><span id="maincontent"></span><h2>Quis#2</h2><div class="box py-3 quizinfo"><p>Attempts allowed: 1</p>
<p>This quiz closed on Tuesday, 10 March 2026, 11:59 PM</p>
</div><h3>Summary of your previous attempts</h3><div class="theme-table-wrap"><table class="generaltable quizattemptsummary table table-striped">
<thead>
<tr>
<th class="header c0" style="text-align:left;" scope="col">State</th>
<th class="header c1" style="text-align:center;" scope="col">Grade / 100.00</th>
<th class="header c2 lastcol" style="text-align:center;" scope="col">Review</th>
</tr>
</thead>
<tbody><tr class="lastrow">
<td class="cell c0" style="text-align:left;">Finished<span class="statedetails">Submitted Tuesday, 24 February 2026, 7:25 PM</span></td>
<td class="cell c1" style="text-align:center;">100.00</td>
<td class="cell c2 lastcol" style="text-align:center;"><span class="noreviewmessage">Not permitted</span></td>
</tr>
</tbody>
</table></div>
<div id="feedback" class="box py-3 generalbox"><h3>Your final grade for this quiz is 100.00/100.00.</h3></div><div class="box py-3 quizattempt"><p>No more attempts are allowed</p>
<div class="continuebutton">
    <form method="get" action="https://elearning.budiluhur.ac.id/course/view.php">
            <input type="hidden" name="id" value="31160">
        <button type="submit" class="btn btn-secondary" id="single_button69b1ab538698b32" title="">Back to the course</button>
    </form>
</div></div></div>```



````

sample of list quiz:

```

<div role="main"><span id="maincontent"></span><h2>Quizzes</h2><div class="theme-table-wrap"><table class="generaltable table table-striped">
<thead>
<tr>
<th class="header c0" style="text-align:center;" scope="col">Topic</th>
<th class="header c1" style="text-align:left;" scope="col">Name</th>
<th class="header c2" style="text-align:left;" scope="col">Quiz closes</th>
<th class="header c3 lastcol" style="text-align:left;" scope="col">Grade</th>
</tr>
</thead>
<tbody><tr class="">
<td class="cell c0" style="text-align:center;">1-Graph sederhana- 03 Maret 2025</td>
<td class="cell c1" style="text-align:left;"><a href="view.php?id=1482498">Quis Pert 1</a></td>
<td class="cell c2" style="text-align:left;">Thursday, 30 April 2026, 11:59 PM</td>
<td class="cell c3 lastcol" style="text-align:left;">60.00/100.00</td>
</tr>
<tr class="">
<td class="cell c0" style="text-align:center;">2-Graph Berarah-24 Februari 2026</td>
<td class="cell c1" style="text-align:left;"><a href="view.php?id=1482502">Quis#2</a></td>
<td class="cell c2" style="text-align:left;">Tuesday, 10 March 2026, 11:59 PM</td>
<td class="cell c3 lastcol" style="text-align:left;">100.00/100.00</td>
</tr>
<tr class="lastrow">
<td class="cell c0" style="text-align:center;">4-Graph Berbobot[03/03/2026]</td>
<td class="cell c1" style="text-align:left;"><a href="view.php?id=1482510">Quis#04</a></td>
<td class="cell c2" style="text-align:left;">Monday, 30 March 2026, 11:59 PM</td>
<td class="cell c3 lastcol" style="text-align:left;"></td>
</tr>
</tbody>
</table></div>
</div>
```

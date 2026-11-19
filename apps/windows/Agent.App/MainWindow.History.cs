using System.Text.Json.Nodes;
using Agent.Core;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Media;
using Microsoft.UI.Xaml.Shapes;
namespace Agent.App;
public sealed partial class MainWindow
{
    private async Task ShowUsageHistoryAsync(bool research)
    {
        var c=Need();long epoch=accountEpoch;string profile=activeProfile;
        if(!research&&profile=="")throw new AgentException("Select an active ChatGPT account first.");
        var range=new ComboBox{Header="History",HorizontalAlignment=HorizontalAlignment.Stretch};foreach(var item in new[]{("24h","24 hours"),("week","1 week"),("period","Entire period")})range.Items.Add(new ComboBoxItem{Content=item.Item2,Tag=item.Item1});range.SelectedIndex=0;
        var windows=new ComboBox{Header="Allowance window",HorizontalAlignment=HorizontalAlignment.Stretch,Visibility=research?Visibility.Collapsed:Visibility.Visible};
        var display=new StackPanel{Spacing=12};var content=new StackPanel{Spacing=12,MinWidth=420};content.Children.Add(range);content.Children.Add(windows);content.Children.Add(display);
        long revision=0;bool filling=false,open=true;string selectedWindow="";
        async Task Read()
        {
            long requestRevision=++revision;display.Children.Clear();display.Children.Add(Text("Loading recorded observations…"));
            try
            {
                string query="range="+Wire.Segment((range.SelectedItem as ComboBoxItem)?.Tag as string??"24h");if(!research)query+="&profile_id="+Wire.Segment(profile)+(selectedWindow==""?"":"&window_id="+Wire.Segment(selectedWindow));
                var result=await c.GetAsync((research?"/api/research/history?":"/api/usage/history?")+query,accountLifetime.Token);
                if(!open||requestRevision!=revision||!IsCurrentSession(c,epoch)||(!research&&activeProfile!=profile))return;
                filling=true;try{windows.Items.Clear();foreach(var item in result.Items("windows")){var option=new ComboBoxItem{Content=item.Text("label"),Tag=item.Text("id")};windows.Items.Add(option);if(item.Text("id")==result.Text("window_id"))windows.SelectedItem=option;}selectedWindow=result.Text("window_id");}finally{filling=false;}
                display.Children.Clear();if(result.Flag("stale"))display.Children.Add(Text("Last-known observations · stale"));if(result.Text("warning")!="")display.Children.Add(Text(result.Text("warning")));
                long tracking=result["tracking_started_at"]?.GetValue<long>()??0;if(tracking>0)display.Children.Add(Text("Tracking started "+DateTimeOffset.FromUnixTimeSeconds(tracking).ToLocalTime().ToString("g"),12));
                var points=result.Items("points").Where(p=>p["at"] is not null&&p["remaining_percent"] is not null).OrderBy(p=>p["at"]!.GetValue<long>()).ToArray();
                if(points.Length==0){display.Children.Add(Text("No recorded observations for this range. Opening history does not send a model request or search."));return;}
                const double width=600,height=180;var canvas=new Canvas{Width=width,Height=height};long first=points[0]["at"]!.GetValue<long>(),last=points[^1]["at"]!.GetValue<long>();
                var line=new Polyline{Stroke=Sage,StrokeThickness=2};long prior=0,gap=result["gap_seconds"]?.GetValue<long>()??0;
                foreach(var point in points)
                {
                    long at=point["at"]!.GetValue<long>();double percent=point["remaining_percent"]!.GetValue<double>();if(!double.IsFinite(percent)||percent<0||percent>100)continue;
                    if(prior>0&&gap>0&&at-prior>gap&&line.Points.Count>0){canvas.Children.Add(line);line=new Polyline{Stroke=Sage,StrokeThickness=2};}
                    line.Points.Add(new Windows.Foundation.Point(8+(width-16)*(at-first)/Math.Max(1d,last-first),8+(height-16)*(100-percent)/100));prior=at;
                }
                canvas.Children.Add(line);var chart=new Viewbox{Child=canvas,Stretch=Stretch.Uniform,MaxHeight=180};Microsoft.UI.Xaml.Automation.AutomationProperties.SetName(chart,"Recorded allowance remaining over time; observations listed below");display.Children.Add(chart);
                display.Children.Add(Text("Recorded observations · local time",12));var observations=new StackPanel{Spacing=8};foreach(var point in points.TakeLast(200)){long at=point["at"]!.GetValue<long>(),reset=point["reset_at"]?.GetValue<long>()??0;observations.Children.Add(Text(DateTimeOffset.FromUnixTimeSeconds(at).ToLocalTime().ToString("g")+" · "+point["remaining_percent"]!.GetValue<double>().ToString("0.#")+"% remaining"+(reset>0?" · reset "+DateTimeOffset.FromUnixTimeSeconds(reset).ToLocalTime().ToString("g"):""),12));}
                if(points.Length>200)display.Children.Add(Text($"Chart includes {points.Length} observations; newest 200 listed."));display.Children.Add(observations);
                if(result.Flag("sessions_truncated"))display.Children.Add(Text("The server truncated session details."));
            }
            catch(OperationCanceledException){}catch{if(open&&IsCurrentSession(c,epoch)){display.Children.Clear();display.Children.Add(Text("Usage history could not be loaded. Change range or reopen to retry. No observation was invented."));}}
        }
        range.SelectionChanged+=async(_,_)=>{if(!filling&&open)await Read();};windows.SelectionChanged+=async(_,_)=>{if(!filling&&open){selectedWindow=(windows.SelectedItem as ComboBoxItem)?.Tag as string??"";await Read();}};
        await Read();try{await AskAsync(research?"Brave Search usage":"ChatGPT usage",new ScrollViewer{MaxHeight=560,Content=content});}finally{open=false;revision++;}
    }
}
